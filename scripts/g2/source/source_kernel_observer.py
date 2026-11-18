"""Bind the observer to the fixed Ubuntu kernel's ELF, BTF and call sites."""
import hashlib
import json
import re
import struct

import source_bpf_core

RELEASE = '6.8.0-147-generic'
_SYMBOL_TYPES = {
    'idr_alloc_cyclic': 'T', 'prog_idr': 'd', 'map_idr': 'd',
    'bpf_prog_free_id': 'T', 'bpf_map_free_id': 'T',
    'cgroup_bpf_offline': 'T', 'cgroup_bpf_release': 't',
    'bpf_audit_prog.part.0': 't', 'audit_log_start': 'T',
    'audit_log_format': 'T', 'audit_log_end': 'T',
    'map_create': 't', 'bpf_prog_load': 't', 'bpf_prog_put_deferred': 't',
    'bpf_map_offload_ops': 'D',
    '__x64_sys_bpf': 'T',
    'map_idr_lock': 'b', 'idr_remove': 'T',
    '_raw_spin_lock_bh': 'T', '_raw_spin_unlock_bh': 'T',
    '_raw_spin_lock_irqsave': 'T', '_raw_spin_unlock_irqrestore': 'T',
}
_BOUNDARIES = {'_stext': 'T', '_etext': 'T', '_sdata': 'D', '_edata': 'D'}
_CONFIG = ('CONFIG_BPF_LSM=y', 'CONFIG_BPF_SYSCALL=y', 'CONFIG_MODVERSIONS=y',
           'CONFIG_AUDIT=y', 'CONFIG_AUDITSYSCALL=y', 'CONFIG_KPROBES=y',
           'CONFIG_KRETPROBES=y', '# CONFIG_MODULE_FORCE_LOAD is not set',
           '# CONFIG_MODULE_FORCE_UNLOAD is not set', '# CONFIG_BPF_PRELOAD is not set',
           '# CONFIG_X86_X32_ABI is not set')


def _need(ok, message):
    if not ok:
        raise ValueError('kernel observer: ' + message)


def _bounded(raw, off, length):
    _need(type(off) is int and type(length) is int and 0 <= off <= len(raw) and
          0 <= length <= len(raw) - off, 'invalid ELF extent')
    return raw[off:off + length]


def _string(data, offset):
    _need(0 <= offset < len(data), 'invalid section name')
    end = data.find(b'\0', offset)
    _need(end >= offset and end - offset <= 4096, 'unterminated section name')
    return data[offset:end].decode('ascii', 'strict')


def decode_kernel(raw):
    """Decode a boot image to elf, btf, build_id and named ELF sections.

    Sections map names to address, offset, size, type and flags; offsets are
    into the returned ELF bytes.  Only the pinned zstd x86-64 boot format is
    accepted, and each section and GNU note is bounded before use.
    """
    from compression import zstd
    _need(type(raw) is bytes and 0x250 <= len(raw) <= 1024 ** 3 and
          raw[0x202:0x206] == b'HdrS' and struct.unpack_from('<H', raw, 0x206)[0] == 0x20f,
          'boot header differs')
    setup = raw[0x1f1]
    start = (setup + 1) * 512 + struct.unpack_from('<I', raw, 0x248)[0]
    size = struct.unpack_from('<I', raw, 0x24c)[0]
    _need(setup > 0 and size >= 8 and start >= (setup + 1) * 512, 'boot payload invalid')
    payload = _bounded(raw, start, size)
    _need(payload[:4] == b'\x28\xb5\x2f\xfd', 'kernel compression differs')
    decoder = zstd.ZstdDecompressor()
    elf = decoder.decompress(payload, max_length=256 * 1024 * 1024)
    _need(decoder.eof and len(decoder.unused_data) == 4 and
          struct.unpack('<I', decoder.unused_data)[0] == len(elf), 'kernel frame invalid')
    _need(len(elf) >= 64 and elf[:7] == b'\x7fELF\x02\x01\x01' and
          struct.unpack_from('<HHI', elf, 16) == (2, 62, 1), 'kernel ELF identity invalid')
    phoff = struct.unpack_from('<Q', elf, 32)[0]
    ehsize, phwidth, phcount = struct.unpack_from('<HHH', elf, 52)
    _need(ehsize == 64 and phwidth == 56 and 0 < phcount <= 128, 'program table invalid')
    ph = _bounded(elf, phoff, phwidth * phcount)
    for index in range(phcount):
        kind, flags, fileoff, address, _, filesz, memsz, align = struct.unpack_from('<IIQQQQQQ', ph, index * phwidth)
        _need(filesz <= memsz and address + memsz < 1 << 64 and
              (align == 0 or align & (align - 1) == 0), 'program segment invalid')
        _bounded(elf, fileoff, filesz)
    off = struct.unpack_from('<Q', elf, 40)[0]
    width, count, names_index = struct.unpack_from('<HHH', elf, 58)
    _need(width == 64 and 0 < count <= 65535 and 0 < names_index < count, 'section table invalid')
    table = _bounded(elf, off, count * width)
    rows = [struct.unpack_from('<IIQQQQIIQQ', table, i * width) for i in range(count)]
    names = rows[names_index]
    _need(names[1] == 3, 'section string table invalid')
    strings = _bounded(elf, names[4], names[5])
    _need(strings[:1] == b'\0' and strings[-1:] == b'\0', 'section string table malformed')
    sections = {}
    for index, row in enumerate(rows):
        name = _string(strings, row[0])
        _need(name not in sections, 'duplicate section name')
        if row[1] != 8:
            _bounded(elf, row[4], row[5])
        _need(row[8] == 0 or row[8] & (row[8] - 1) == 0, 'section alignment invalid')
        sections[name] = {'address': row[3], 'offset': row[4], 'size': row[5],
                          'type': row[1], 'flags': row[2]}
        if index == 0:
            _need(row == (0,) * 10, 'null section invalid')
    _need('.BTF' in sections and sections['.BTF']['type'] == 1 and sections['.BTF']['size'] > 0 and
          '.notes' in sections and sections['.notes']['type'] == 7, 'kernel BTF or notes missing')
    btf_section, note_section = sections['.BTF'], sections['.notes']
    btf = _bounded(elf, btf_section['offset'], btf_section['size'])
    source_bpf_core._BTF(btf)
    notes = _bounded(elf, note_section['offset'], note_section['size'])
    ids, pos = [], 0
    while pos < len(notes):
        _need(len(notes) - pos >= 12, 'truncated ELF note')
        namesz, descsz, kind = struct.unpack_from('<III', notes, pos)
        pos += 12
        name = _bounded(notes, pos, namesz)
        pos += (namesz + 3) & ~3
        desc = _bounded(notes, pos, descsz)
        pos += (descsz + 3) & ~3
        _need(pos <= len(notes), 'truncated ELF note padding')
        if name == b'GNU\0' and kind == 3:
            _need(len(desc) == 20, 'unexpected GNU build ID size')
            ids.append(desc.hex())
    _need(pos == len(notes) and len(ids) == 1, 'missing or ambiguous GNU build ID')
    return {'elf': elf, 'btf': btf, 'build_id': ids[0], 'sections': sections}


def _map(raw):
    _need(type(raw) is bytes and 0 < len(raw) <= 32 * 1024 * 1024, 'System.map size invalid')
    found, addresses = {}, []
    needed = {**_SYMBOL_TYPES, **_BOUNDARIES}
    for line in raw.decode('ascii', 'strict').splitlines():
        match = re.fullmatch(r'([0-9a-f]{16}) ([A-Za-z]) (\S+)', line)
        _need(match is not None, 'malformed System.map line')
        address, kind, name = int(match[1], 16), match[2], match[3]
        if name in needed:
            _need(name not in found and kind == needed[name], 'ambiguous or wrong-type symbol: ' + name)
            found[name] = address
        if kind in ('t', 'T') and address >= 0xffffffff80000000:
            addresses.append(address)
    _need(set(found) == set(needed), 'required System.map symbols missing')
    return found, sorted(set(addresses))


def _member(btf, type_id, name):
    matches = btf.member(type_id, name)
    _need(len(matches) == 1 and matches[0][1] % 8 == 0 and matches[0][2] == 0,
          'BTF member invalid: ' + name)
    return matches[0][0], matches[0][1] // 8


_PINNED_PUBLICATION_LAYOUT = {'bpf_map_btf_key_type_id_offset': 68,
 'bpf_map_btf_key_type_id_size': 4,
 'bpf_map_btf_offset': 80,
 'bpf_map_btf_size': 8,
 'bpf_map_btf_value_type_id_offset': 72,
 'bpf_map_btf_value_type_id_size': 4,
 'bpf_map_key_size_offset': 28,
 'bpf_map_key_size_size': 4,
 'bpf_map_map_extra_offset': 40,
 'bpf_map_map_extra_size': 8,
 'bpf_map_map_flags_offset': 48,
 'bpf_map_map_flags_size': 4,
 'bpf_map_map_type_offset': 24,
 'bpf_map_map_type_size': 4,
 'bpf_map_max_entries_offset': 36,
 'bpf_map_max_entries_size': 4,
 'bpf_map_name_offset': 96,
 'bpf_map_name_size': 16,
 'bpf_map_ops_offset': 0,
 'bpf_map_ops_size': 8,
 'bpf_map_struct_size': 320,
 'bpf_map_value_size_offset': 32,
 'bpf_map_value_size_size': 4,
 'bpf_prog_aux_attach_btf_id_offset': 48,
 'bpf_prog_aux_attach_btf_id_size': 4,
 'bpf_prog_aux_load_time_offset': 928,
 'bpf_prog_aux_load_time_size': 8,
 'bpf_prog_aux_name_offset': 960,
 'bpf_prog_aux_name_size': 16,
 'bpf_prog_aux_offload_offset': 992,
 'bpf_prog_aux_offload_size': 8,
 'bpf_prog_aux_offload_requested_offset': 138,
 'bpf_prog_aux_offload_requested_size': 1,
 'bpf_prog_aux_offset': 56,
 'bpf_prog_aux_size': 8,
 'bpf_prog_aux_user_offset': 920,
 'bpf_prog_aux_user_size': 8,
 'bpf_prog_aux_verified_insns_offset': 936,
 'bpf_prog_aux_verified_insns_size': 4,
 'bpf_prog_len_offset': 12,
 'bpf_prog_len_size': 4,
 'bpf_prog_struct_size': 72,
 'bpf_prog_aux_struct_size': 1112,
 'bpf_prog_tag_offset': 20,
 'bpf_prog_tag_size': 8,
 'bpf_prog_type_offset': 4,
 'bpf_prog_type_size': 4,
 'btf_id_offset': 88,
 'btf_id_size': 4,
 'btf_struct_size': 216,
 'kuid_struct_size': 4,
 'kuid_val_offset': 0,
 'kuid_val_size': 4,
 'user_struct_struct_size': 144,
 'user_struct_uid_offset': 80,
 'user_struct_uid_size': 4}

def _publication_layout(btf):
    """Validate every publication read against the selected kernel's BTF."""
    types = {name: btf.unique(4, name) for name in
             ('bpf_prog', 'bpf_prog_aux', 'user_struct', 'bpf_map', 'btf')}
    types['kuid'] = btf.unique(8, 'kuid_t')
    _need(btf.resolve(types['kuid'])[0] == btf.resolve(_member(btf, types['user_struct'], 'uid')[0])[0],
          'creator UID type differs')
    result = {name + '_struct_size': btf.size(ident) for name, ident in types.items()}
    scalars = {'bpf_prog': ('len',),
               'bpf_prog_aux': ('load_time', 'verified_insns', 'attach_btf_id'),
               'kuid': ('val',),
               'bpf_map': ('key_size', 'value_size', 'max_entries', 'map_flags', 'map_extra',
                           'btf_key_type_id', 'btf_value_type_id'), 'btf': ('id',)}
    for owner, fields in scalars.items():
        for field in fields:
            typ, offset = _member(btf, types[owner], field)
            _, scalar = btf.resolve(typ)
            width = 8 if field in ('load_time', 'map_extra') else 4
            _need(scalar['kind'] == 1 and scalar['value'] == width and
                  struct.unpack('<I', scalar['data'])[0] == width * 8,
                  'publication scalar differs: ' + owner + '.' + field)
            result[owner + '_' + field + '_offset'] = offset
            result[owner + '_' + field + '_size'] = width
    boolean_type, boolean_offset = _member(btf, types['bpf_prog_aux'], 'offload_requested')
    _, boolean = btf.resolve(boolean_type)
    _need(boolean['kind'] == 1 and boolean['name'] == '_Bool' and boolean['value'] == 1 and
          struct.unpack('<I', boolean['data'])[0] == 0x04000008,
          'publication offload predicate type differs')
    result['bpf_prog_aux_offload_requested_offset'] = boolean_offset
    result['bpf_prog_aux_offload_requested_size'] = 1
    for owner, field, enum_name in (('bpf_prog', 'type', 'bpf_prog_type'),
                                    ('bpf_map', 'map_type', 'bpf_map_type')):
        typ, offset = _member(btf, types[owner], field)
        _, enum = btf.resolve(typ)
        _need(enum['kind'] == 6 and enum['value'] == 4 and enum['name'] == enum_name,
              'publication enum differs')
        result[owner + '_' + field + '_offset'] = offset
        result[owner + '_' + field + '_size'] = 4
    for owner, field, target in (('bpf_prog', 'aux', 'bpf_prog_aux'),
                                  ('bpf_prog_aux', 'user', 'user_struct'),
                                  ('bpf_prog_aux', 'offload', 'bpf_prog_offload'),
                                  ('bpf_map', 'ops', 'bpf_map_ops'), ('bpf_map', 'btf', 'btf')):
        typ, offset = _member(btf, types[owner], field)
        _, pointer = btf.resolve(typ)
        _need(pointer['kind'] == 2 and btf.size(typ) == 8 and
              btf.resolve(pointer['value'])[0] == btf.unique(4, target),
              'publication pointer target differs: ' + owner + '.' + field)
        result[owner + '_' + field + '_offset'] = offset
        result[owner + '_' + field + '_size'] = 8
    uid_type, uid_offset = _member(btf, types['user_struct'], 'uid')
    result['user_struct_uid_offset'] = uid_offset
    result['user_struct_uid_size'] = btf.size(uid_type)
    for owner, field, count in (('bpf_prog', 'tag', 8), ('bpf_prog_aux', 'name', 16),
                                  ('bpf_map', 'name', 16)):
        typ, offset = _member(btf, types[owner], field)
        _, array = btf.resolve(typ)
        _need(array['kind'] == 3, 'publication array type differs')
        element, index, length = struct.unpack('<III', array['data'])
        _, byte = btf.resolve(element)
        _need(length == count and byte['kind'] == 1 and byte['value'] == 1 and
              btf.size(index) == 4, 'publication array width differs')
        result[owner + '_' + field + '_offset'] = offset
        result[owner + '_' + field + '_size'] = count
    return result


def _layout(raw):
    btf = source_bpf_core._BTF(raw)
    ident = btf.unique(4, 'idr')
    idr_offsets = {}
    for field in ('idr_base', 'idr_next'):
        typ, offset = _member(btf, ident, field)
        _, scalar = btf.resolve(typ)
        _need(scalar['kind'] == 1 and scalar['value'] == 4 and
              struct.unpack('<I', scalar['data'])[0] == 32,
              'IDR cursor scalar differs: ' + field)
        idr_offsets[field + '_offset'] = offset
    buffer = btf.unique(4, 'audit_buffer')
    stamp = btf.unique(4, 'audit_stamp')
    context = btf.unique(4, 'audit_context')
    ctx_type, ctx_offset = _member(btf, buffer, 'ctx')
    _, ctx_pointer = btf.resolve(ctx_type)
    _need(ctx_pointer['kind'] == 2 and btf.resolve(ctx_pointer['value'])[0] == context,
          'audit buffer context pointer differs')
    stamp_type, stamp_offset = _member(btf, buffer, 'stamp')
    _need(btf.resolve(stamp_type)[0] == stamp, 'audit buffer stamp type differs')
    time_type, time_offset = _member(btf, stamp, 'ctime')
    sec_type, sec = _member(btf, time_type, 'tv_sec')
    nsec_type, nsec = _member(btf, time_type, 'tv_nsec')
    serial_type, serial = _member(btf, stamp, 'serial')
    class_type, class_offset = _member(btf, context, 'context')
    ctx_stamp, ctx_stamp_offset = _member(btf, context, 'stamp')
    major_type, major_offset = _member(btf, context, 'major')
    argv_type, argv_offset = _member(btf, context, 'argv')
    retcode_type, retcode_offset = _member(btf, context, 'return_code')
    retvalid_type, retvalid_offset = _member(btf, context, 'return_valid')
    arch_type, arch_offset = _member(btf, context, 'arch')
    _, argv_resolved = btf.resolve(argv_type)
    _need(argv_resolved['kind'] == 3, 'audit argv type differs')
    argv_elem, argv_index, argv_count = struct.unpack('<III', argv_resolved['data'])
    _, argv_elem_resolved = btf.resolve(argv_elem)
    _need(argv_count == 4 and argv_elem_resolved['kind'] == 1 and argv_elem_resolved['value'] == 8 and
          struct.unpack('<I', argv_elem_resolved['data'])[0] == 64 and btf.size(argv_index) == 4,
          'audit argv array layout differs')
    for sc_type, sc_width, sc_signed in ((major_type, 4, True), (retcode_type, 8, True),
                                         (retvalid_type, 4, True), (arch_type, 4, True)):
        _, sc = btf.resolve(sc_type)
        sc_enc = struct.unpack('<I', sc['data'])[0]
        _need(sc['kind'] == 1 and sc['value'] == sc_width and
              bool((sc_enc >> 24) & 1) == sc_signed and (sc_enc & 0xffff) == sc_width * 8,
              'audit syscall context scalar differs')
    _need(btf.resolve(ctx_stamp)[0] == stamp and btf.size(sec_type) == 8 and
          btf.size(nsec_type) == 8 and btf.size(serial_type) == 4, 'audit stamp fields differ')
    for type_id, signed in ((sec_type, True), (nsec_type, True), (serial_type, False)):
        _, integer = btf.resolve(type_id)
        _need(integer['kind'] == 1 and len(integer['data']) == 4, 'audit scalar type differs')
        encoding = struct.unpack('<I', integer['data'])[0]
        _need(bool((encoding >> 24) & 1) == signed and
              (encoding & 0xffff) == integer['value'] * 8,
              'audit scalar signedness or width differs')
    _, enum = btf.resolve(class_type)
    _need(enum['kind'] == 6 and enum['value'] == 4, 'audit context enum differs')
    values = {}
    for pos in range(0, len(enum['data']), 8):
        name, value = struct.unpack_from('<II', enum['data'], pos)
        name = source_bpf_core._string(btf.strings, name)
        _need(name not in values, 'duplicate audit context enum')
        values[name] = value
    _need(all(name in values for name in ('AUDIT_CTX_UNUSED', 'AUDIT_CTX_SYSCALL', 'AUDIT_CTX_URING')),
          'audit context enum values missing')
    return {'idr_size': btf.size(ident), **idr_offsets, 'spinlock_size': btf.size(btf.unique(8, 'spinlock_t')), 'audit_buffer_size': btf.size(buffer),
            'audit_buffer_ctx_offset': ctx_offset, 'audit_buffer_stamp_offset': stamp_offset,
            'audit_stamp_size': btf.size(stamp), 'stamp_seconds_offset': time_offset + sec,
            'stamp_nanoseconds_offset': time_offset + nsec, 'stamp_serial_offset': serial,
            'stamp_serial_size': btf.size(serial_type), 'audit_context_size': btf.size(context),
            'context_class_offset': class_offset, 'context_stamp_offset': ctx_stamp_offset,
            'context_major_offset': major_offset, 'context_major_size': 4,
            'context_argv_offset': argv_offset, 'context_argv_size': 32,
            'context_return_code_offset': retcode_offset, 'context_return_code_size': 8,
            'context_return_valid_offset': retvalid_offset, 'context_return_valid_size': 4,
            'context_arch_offset': arch_offset, 'context_arch_size': 4,
            'context_unused_value': values['AUDIT_CTX_UNUSED'],
            'context_syscall_value': values['AUDIT_CTX_SYSCALL'],
            'context_uring_value': values['AUDIT_CTX_URING'], **_publication_layout(btf)}


def _read_address(decoded, address, size):
    candidates = [s for s in decoded['sections'].values() if s['type'] == 1 and
                  s['address'] <= address and address + size <= s['address'] + s['size'] and s['flags'] & 2]
    _need(len(candidates) == 1, 'address not in a unique file-backed allocated section')
    section = candidates[0]
    return _bounded(decoded['elf'], section['offset'] + address - section['address'], size)


def observer_bindings(kernel, system_map, config, headers_manifest, symvers):
    """Generate the sole canonical observer input from original pinned bytes.

    headers_manifest is the 64-character digest of the previously verified
    complete header inventory; symvers is the original Module.symvers bytes.
    """
    _need(type(config) is bytes and 0 < len(config) <= 1024 * 1024, 'config invalid')
    lines = config.decode('ascii', 'strict').splitlines()
    _need(all(lines.count(line) == 1 for line in _CONFIG), 'config prerequisite missing')
    _need(type(headers_manifest) is str and re.fullmatch('[0-9a-f]{64}', headers_manifest) is not None and
          headers_manifest != '0' * 64, 'header manifest digest invalid')
    _need(type(symvers) is bytes and 0 < len(symvers) <= 32 * 1024 * 1024, 'symvers invalid')
    exports = [row.split() for row in symvers.decode('ascii', 'strict').splitlines()]
    _need(sum(len(row) >= 4 and row[1] == 'idr_alloc_cyclic' and row[2] == 'vmlinux' for row in exports) == 1,
          'idr anchor export missing or ambiguous')
    decoded = decode_kernel(kernel)
    symbols, text_addresses = _map(system_map)
    anchor = symbols['idr_alloc_cyclic']
    _need(symbols['_stext'] < anchor < symbols['_etext'] and symbols['_sdata'] < symbols['_edata'],
          'kernel core ranges invalid')
    for name, kind in _SYMBOL_TYPES.items():
        address = symbols[name]
        if kind in ('T', 't'):
            _need(symbols['_stext'] <= address < symbols['_etext'], 'text symbol outside core')
            _read_address(decoded, address, 1)
        elif name == 'bpf_map_offload_ops':
            section = decoded['sections'].get('.rodata')
            _need(section is not None and section['type'] == 1 and
                  section['address'] <= address and address + 8 <= section['address'] + section['size'] and
                  address % 8 == 0, 'offload table outside pinned rodata')
            _read_address(decoded, address, 8)
        elif name == 'map_idr_lock':
            section = decoded['sections'].get('.bss')
            _need(section is not None and section['type'] == 8 and section['flags'] & 3 == 3 and
                  section['address'] <= address and address + 4 <= section['address'] + section['size'] and
                  address % 4 == 0, 'map IDR lock outside pinned BSS')
        else:
            _need(symbols['_sdata'] <= address and address + 24 <= symbols['_edata'] and address % 8 == 0,
                  'IDR symbol outside core data')
            _read_address(decoded, address, 24)
    # verifier's returned program is retained in RBX and the stack slot;
    # the split body retains its program/op in R13/R12, loads the program ID,
    # and forwards the returned audit buffer through format and end.
    windows = (
        ('map_create', 0x1d3, '498b45084889dfe88137f5004989c7'),
        ('bpf_prog_load', 0x6e4, '48894424384889c385c9'),
        ('bpf_prog_load', 0x791, '488b5c243885d2740f'),
        ('bpf_prog_put_deferred', 0x13, '488b9f58ffffff4889df'),
        ('bpf_audit_prog.part.0', 0x05, '554889e5415641554989fd41544189f453'),
        ('bpf_audit_prog.part.0', 0x48, '4589e64183fc01774f498b45384a8b0cf5f0a06382'),
        ('bpf_audit_prog.part.0', 0x8e, '65488b042540430300488bb8980c0000eb91'),
    )
    allocator_windows = (
        ('map_create', 0x319, '48c7c714b00984e82b28f50041b820080000b9ffffff7f4c89feba0100000048c7c760008183e8cc55e9004189c485c07e044189473448c7c714b00984e86529f50065ff0d', (3, 34, 57)),
        ('bpf_map_free_id', 0x21, '48c7c714b00984e83334f5008b733448c7c7600081834989c4e8616ae900c74334000000004c89e648c7c714b00984e8ab34f5005b415c5d31', (3, 18, 43)),
    )
    for owner, offset, raw_hex, _ in allocator_windows:
        raw = bytes.fromhex(raw_hex)
        _need(_read_address(decoded, symbols[owner] + offset, len(raw)) == raw,
              'allocator lock window differs: ' + owner)
    for owner, offset, callee in (('map_create', 0x320, '_raw_spin_lock_bh'),
                                  ('map_create', 0x356, '_raw_spin_unlock_bh'),
                                  ('bpf_map_free_id', 0x28, '_raw_spin_lock_irqsave'),
                                  ('bpf_map_free_id', 0x3a, 'idr_remove'),
                                  ('bpf_map_free_id', 0x50, '_raw_spin_unlock_irqrestore')):
        address = symbols[owner] + offset
        call = _read_address(decoded, address, 5)
        _need(call[0] == 0xe8 and address + 5 + struct.unpack('<i', call[1:])[0] == symbols[callee],
              'allocator call target differs: ' + owner)
    for owner, offset, raw_hex in windows:
        expected = bytes.fromhex(raw_hex)
        _need(_read_address(decoded, symbols[owner] + offset, len(expected)) == expected,
              'instruction provenance differs: ' + owner)
    # Exact known instruction windows establish the register sources and
    # constants; the call target is decoded independently from the E8 rel32.
    # These windows also bind the ID stores to the same object after return.
    patterns = [
        ('map_create', 0x325, 'idr_alloc_cyclic', '41b820080000b9ffffff7f4c89feba0100000048c7c7', 'map_idr', '4189c485c07e0441894734'),
        ('bpf_prog_load', 0x70a, 'idr_alloc_cyclic', '41b820080000b9ffffff7f4889deba0100000048c7c7', 'prog_idr', '4189c485c07e08488b433844896020'),
        ('bpf_prog_load', 0x79a, 'bpf_audit_prog.part.0', '4889df31f6', None, ''),
        ('bpf_prog_put_deferred', 0x2c, 'bpf_audit_prog.part.0', 'be010000004889df', None, ''),
        ('bpf_audit_prog.part.0', 0x2f, 'audit_log_start', '31ffba36050000be20080000', None, '4889c34885c0'),
        ('bpf_audit_prog.part.0', 0x5d, 'audit_log_format', '4889df48c7c6d95fbf828b5020', None, '4889df'),
        ('bpf_audit_prog.part.0', 0x6f, 'audit_log_end', '4889df', None, ''),
    ]
    # Explicit call offsets are relative to the instruction window start.
    calls = []
    for owner, start_offset, callee, prefix_hex, data_symbol, suffix_hex in patterns:
        start = symbols[owner] + start_offset
        prefix = bytes.fromhex(prefix_hex)
        if data_symbol is not None:
            prefix += struct.pack('<I', symbols[data_symbol] & 0xffffffff)
        _need(_read_address(decoded, start, len(prefix)) == prefix, 'instruction dataflow differs: ' + owner)
        call_address = start + len(prefix)
        call = _read_address(decoded, call_address, 5)
        _need(call[0] == 0xe8 and call_address + 5 + struct.unpack('<i', call[1:])[0] == symbols[callee],
              'call target differs: ' + owner)
        suffix = bytes.fromhex(suffix_hex)
        _need(_read_address(decoded, call_address + 5, len(suffix)) == suffix, 'post-call dataflow differs')
        next_symbols = [value for value in text_addresses if value > symbols[owner]]
        _need(next_symbols and call_address + 5 + len(suffix) <= next_symbols[0], 'call outside owner')
        calls.append({'owner': owner, 'callee': callee, 'call_delta': call_address - anchor,
                      'return_delta': call_address + 5 - anchor, 'bytes': call.hex()})
    layout = _layout(decoded['btf'])
    audit_layout = {key: value for key, value in layout.items()
                    if key not in _PINNED_PUBLICATION_LAYOUT}
    _need(audit_layout == {'idr_size': 24, 'idr_base_offset': 16, 'idr_next_offset': 20, 'spinlock_size': 4, 'audit_buffer_size': 72, 'audit_buffer_ctx_offset': 32,
                     'audit_buffer_stamp_offset': 40, 'audit_stamp_size': 24,
                     'stamp_seconds_offset': 0, 'stamp_nanoseconds_offset': 8,
                     'stamp_serial_offset': 16, 'stamp_serial_size': 4,
                     'audit_context_size': 1200, 'context_class_offset': 4, 'context_stamp_offset': 16,
                     'context_major_offset': 40, 'context_major_size': 4,
                     'context_argv_offset': 48, 'context_argv_size': 32,
                     'context_return_code_offset': 80, 'context_return_code_size': 8,
                     'context_return_valid_offset': 96, 'context_return_valid_size': 4,
                     'context_arch_offset': 952, 'context_arch_size': 4,
                     'context_unused_value': 0, 'context_syscall_value': 1, 'context_uring_value': 2},
          'pinned audit and IDR layout differs')
    _need({key: value for key, value in layout.items() if key not in audit_layout} ==
          _PINNED_PUBLICATION_LAYOUT, 'pinned publication layout differs')
    return {'version': 1, 'kernel_release': RELEASE, 'kernel_sha256': hashlib.sha256(kernel).hexdigest(),
            'system_map_sha256': hashlib.sha256(system_map).hexdigest(),
            'config_sha256': hashlib.sha256(config).hexdigest(),
            'kernel_btf_sha256': hashlib.sha256(decoded['btf']).hexdigest(), 'gnu_build_id': decoded['build_id'],
            'headers_manifest_sha256': headers_manifest, 'symvers_sha256': hashlib.sha256(symvers).hexdigest(),
            'anchor': {'name': 'idr_alloc_cyclic', 'type': 'T'},
            'core': {key + '_delta': symbols[name] - anchor for key, name in (
                ('text_start', '_stext'), ('text_end', '_etext'), ('data_start', '_sdata'), ('data_end', '_edata'))} | {
                'bss_start_delta': decoded['sections']['.bss']['address'] - anchor,
                'bss_end_delta': decoded['sections']['.bss']['address'] + decoded['sections']['.bss']['size'] - anchor},
            'symbols': [{'name': name, 'type': kind, 'delta': symbols[name] - anchor}
                        for name, kind in sorted(_SYMBOL_TYPES.items()) if name != 'idr_alloc_cyclic'],
            'calls': sorted(calls, key=lambda value: value['return_delta']), 'layout': layout,
            'allocator_windows': [{'delta': symbols[owner] + offset - anchor, 'bytes': raw_hex, 'relocations': list(relocs)}
                                  for owner, offset, raw_hex, relocs in allocator_windows]}


def binding_bytes(binding):
    result = json.dumps(binding, sort_keys=True, separators=(',', ':'), ensure_ascii=True, allow_nan=False).encode('ascii')
    _need(0 < len(result) <= 16384, 'binding exceeds fixed limit')
    return result


def generated_header(binding):
    """Return a deterministic C header embedding the exact canonical binding."""
    body = binding_bytes(binding)
    names = {entry['name']: entry['delta'] for entry in binding['symbols']}
    names['idr_alloc_cyclic'] = 0
    lines = ['#ifndef G2_OBSERVER_BINDINGS_GENERATED_H', '#define G2_OBSERVER_BINDINGS_GENERATED_H', '']
    for name, value in sorted(names.items()):
        lines.append('#define G2_OBS_%s_DELTA (%dLL)' % (re.sub('[^A-Za-z0-9]', '_', name).upper(), value))
    for name, value in sorted(binding['core'].items()):
        lines.append('#define G2_OBS_CORE_%s (%dLL)' % (name.upper(), value))
    for name, value in sorted(binding['layout'].items()):
        lines.append('#define G2_OBS_LAYOUT_%s (%dU)' % (name.upper(), value))
    lines.extend(['', 'struct g2_observer_call { long long call_delta, return_delta, callee_delta; unsigned char bytes[5]; };',
                  'static const struct g2_observer_call g2_observer_calls[] = {'])
    for call in binding['calls']:
        encoded = ','.join('0x%02x' % value for value in bytes.fromhex(call['bytes']))
        lines.append('    { %dLL, %dLL, %dLL, { %s } },' %
                     (call['call_delta'], call['return_delta'], names[call['callee']], encoded))
    lines.extend(['};', 'struct g2_observer_window { long long delta; unsigned int size; unsigned int reloc[3]; unsigned char bytes[80]; };',
                  'static const struct g2_observer_window g2_observer_windows[] = {'])
    for window in binding['allocator_windows']:
        encoded = ','.join('0x%02x' % value for value in bytes.fromhex(window['bytes']))
        lines.append('    { %dLL, %dU, { %s }, { %s } },' %
                     (window['delta'], len(bytes.fromhex(window['bytes'])),
                      ','.join(str(x) for x in window['relocations']), encoded))
    lines.extend(['};', '#define G2_OBSERVER_CALL_COUNT %dU' % len(binding['calls']),
                  '#define G2_OBSERVER_BINDING_SIZE %dU' % len(body),
                  'static const unsigned char g2_observer_binding[G2_OBSERVER_BINDING_SIZE]',
                  '__attribute__((section(".g2_observer_binding"), used)) = {'])
    for pos in range(0, len(body), 16):
        lines.append('    ' + ','.join('0x%02x' % value for value in body[pos:pos + 16]) + ',')
    lines.extend(['};', '#endif', ''])
    return '\n'.join(lines).encode('ascii')
