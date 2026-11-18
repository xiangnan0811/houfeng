"""Independently relocate the three fixed gate programs from pinned ELF/BTF."""
import struct


HOOKS = {'lsm/bpf': ('bpf', 'g2_bpf'),
         'lsm/inode_mkdir': ('inode_mkdir', 'g2_inode_mkdir'),
         'lsm/inode_rmdir': ('inode_rmdir', 'g2_inode_rmdir')}
MAPS = {'broker_token', 'guard_liveness', 'realm_targets', 'retire_tasks'}


def _require(condition, reason):
    if not condition:
        raise ValueError('BPF policy relocation: ' + reason)


def _slice(raw, offset, length):
    _require(type(offset) is int and type(length) is int and offset >= 0 and length >= 0 and
             offset + length <= len(raw), 'section extent invalid')
    return raw[offset:offset + length]


def _string(raw, offset):
    _require(0 <= offset < len(raw), 'string offset invalid')
    end = raw.find(b'\0', offset)
    _require(end >= offset and end - offset <= 4096, 'string terminator missing')
    return raw[offset:end].decode('ascii', 'strict')


class _ELF:
    def __init__(self, raw):
        _require(type(raw) is bytes and 64 <= len(raw) <= 64 * 1024 * 1024 and
                 raw[:7] == b'\x7fELF\x02\x01\x01', 'ELF identity invalid')
        _require(struct.unpack_from('<HHI', raw, 16) == (1, 247, 1), 'ELF target is not relocatable BPF')
        _require(struct.unpack_from('<H', raw, 52)[0] == 64, 'ELF header size invalid')
        start = struct.unpack_from('<Q', raw, 40)[0]
        width, count, names_index = struct.unpack_from('<HHH', raw, 58)
        _require(width == 64 and 0 < count <= 4096 and 0 < names_index < count, 'ELF geometry invalid')
        table = _slice(raw, start, width * count)
        headers = [struct.unpack_from('<IIQQQQIIQQ', table, i * width) for i in range(count)]
        names_header = headers[names_index]
        _require(names_header[1] == 3, 'section names table invalid')
        names = _slice(raw, names_header[4], names_header[5])
        _require(names[:1] == b'\0' and names[-1:] == b'\0', 'section string table malformed')
        self.sections = []
        self.named = {}
        for index, header in enumerate(headers):
            name = _string(names, header[0])
            _require(name not in self.named, 'duplicate ELF section')
            body = b'' if header[1] == 8 else _slice(raw, header[4], header[5])
            section = {'index': index, 'name': name, 'type': header[1], 'flags': header[2],
                       'link': header[6], 'info': header[7], 'entry_size': header[9], 'body': body}
            self.sections.append(section)
            self.named[name] = section
        for section in self.sections:
            if section['flags'] & 4 and section['body']:
                _require(section['name'] in HOOKS, 'unknown executable section')
        for name in ('.BTF', '.BTF.ext', '.symtab', 'license', '.maps', *HOOKS):
            _require(name in self.named, 'required ELF section missing')
        _require(all(self.named[name]['type'] == 1 for name in ('.BTF', '.BTF.ext', 'license', '.maps', *HOOKS)) and
                 all(self.named[name]['flags'] & 6 == 6 for name in HOOKS),
                 'required ELF section type or flags differ')
        _require(self.named['license']['body'] == b'GPL\0', 'license differs')
        symbols = self.named['.symtab']
        _require(symbols['type'] == 2 and symbols['entry_size'] == 24 and
                 len(symbols['body']) % 24 == 0 and symbols['link'] < count, 'symbol table invalid')
        strings = self.sections[symbols['link']]
        _require(strings['type'] == 3, 'symbol strings invalid')
        _require(strings['body'][:1] == b'\0' and strings['body'][-1:] == b'\0', 'symbol string table malformed')
        self.symbols = []
        for offset in range(0, len(symbols['body']), 24):
            name, info, other, section, value, size = struct.unpack_from('<IBBHQQ', symbols['body'], offset)
            self.symbols.append({'name': _string(strings['body'], name), 'info': info, 'other': other,
                                 'section': section, 'value': value, 'size': size})
        for section_name, (_, function_name) in HOOKS.items():
            section = self.named[section_name]
            matches = [symbol for symbol in self.symbols if symbol['name'] == function_name]
            _require(len(matches) == 1 and matches[0]['section'] == section['index'] and
                     matches[0]['value'] == 0 and matches[0]['size'] == len(section['body']) and
                     matches[0]['info'] & 15 == 2, 'LSM entry point differs')


class _BTF:
    def __init__(self, raw):
        _require(type(raw) is bytes and 24 <= len(raw) <= 64 * 1024 * 1024, 'BTF size invalid')
        magic, version, flags, header, toff, tlen, soff, slen = struct.unpack_from('<HBBIIIII', raw)
        _require((magic, version, flags, header) == (0xeb9f, 1, 0, 24) and
                 toff == 0 and soff == tlen and header + soff + slen == len(raw), 'BTF geometry invalid')
        self.strings = _slice(raw, header + soff, slen)
        _require(self.strings[:1] == b'\0' and self.strings[-1:] == b'\0', 'BTF string table malformed')
        data = _slice(raw, header + toff, tlen)
        self.types = [None]
        self.named = {}
        cursor = 0
        while cursor < len(data):
            _require(cursor + 12 <= len(data) and len(self.types) <= 1048576, 'BTF type header invalid')
            name, info, value = struct.unpack_from('<III', data, cursor)
            kind, vlen, flag = (info >> 24) & 31, info & 65535, info >> 31
            _require(info & 0x60ff0000 == 0 and 1 <= kind <= 19, 'BTF type flags invalid')
            widths = {1: 4, 2: 0, 3: 12, 4: vlen * 12, 5: vlen * 12, 6: vlen * 8,
                      7: 0, 8: 0, 9: 0, 10: 0, 11: 0, 12: 0, 13: vlen * 8,
                      14: 4, 15: vlen * 12, 16: 0, 17: 4, 18: 0, 19: vlen * 12}
            payload = _slice(data, cursor + 12, widths[kind])
            record = {'name': _string(self.strings, name), 'kind': kind, 'value': value,
                      'vlen': vlen, 'flag': flag, 'data': payload}
            self.types.append(record)
            if record['name']:
                self.named.setdefault((kind, record['name']), []).append(len(self.types) - 1)
            cursor += 12 + len(payload)
        _require(cursor == len(data), 'BTF tail invalid')

    def get(self, number):
        _require(type(number) is int and 0 < number < len(self.types), 'BTF type ID invalid')
        return self.types[number]

    def resolve(self, number):
        seen = set()
        while True:
            _require(number not in seen and len(seen) < 64, 'BTF qualifier cycle')
            seen.add(number)
            value = self.get(number)
            if value['kind'] not in (8, 9, 10, 11, 18):
                return number, value
            number = value['value']

    def size(self, number):
        _, value = self.resolve(number)
        if value['kind'] == 2:
            return 8
        _require(value['kind'] in (1, 4, 5, 6, 16, 19), 'unsupported field size')
        return value['value']

    def members(self, number):
        _, value = self.resolve(number)
        _require(value['kind'] in (4, 5), 'field parent is not a composite')
        result = []
        for offset in range(0, len(value['data']), 12):
            name, field_type, bits = struct.unpack_from('<III', value['data'], offset)
            bit_size = bits >> 24 if value['flag'] else 0
            bit_offset = bits & 0xffffff if value['flag'] else bits
            result.append((_string(self.strings, name), field_type, bit_offset, bit_size))
        return result

    def member(self, number, name, seen=()):
        number, _ = self.resolve(number)
        _require(number not in seen and len(seen) < 32, 'anonymous composite cycle')
        matches = []
        for field_name, field_type, bits, bit_size in self.members(number):
            if field_name == name:
                matches.append((field_type, bits, bit_size))
            elif not field_name:
                _, nested = self.resolve(field_type)
                if nested['kind'] in (4, 5):
                    for nested_type, nested_bits, nested_size in self.member(field_type, name, seen + (number,)):
                        matches.append((nested_type, bits + nested_bits, nested_size))
        return matches

    def unique(self, kind, name):
        matches = self.named.get((kind, name), [])
        _require(len(matches) == 1, 'missing or ambiguous target type')
        return matches[0]


def _field_offset(local, target, type_id, access):
    _require(access and access[0] == 0 and len(access) <= 16, 'unsupported field access')
    local_id, local_root = local.resolve(type_id)
    _require(local_root['kind'] in (4, 5) and local_root['name'], 'unnamed field root')
    target_id = target.unique(local_root['kind'], local_root['name'])
    local_bits = target_bits = 0
    for index in access[1:]:
        members = local.members(local_id)
        _require(0 <= index < len(members), 'local member index invalid')
        name, local_id, bits, bit_size = members[index]
        _require(name and bit_size == 0, 'unsupported local anonymous or bit field')
        matches = target.member(target_id, name)
        _require(len(matches) == 1, 'missing or ambiguous target member')
        target_id, actual_bits, actual_bit_size = matches[0]
        _require(actual_bit_size == 0, 'target bit field is unsupported')
        local_bits += bits
        target_bits += actual_bits
    _require(len(access) >= 2 and local_bits % 8 == 0 and target_bits % 8 == 0 and
             local.size(local_id) == target.size(target_id), 'field width or alignment differs')
    _, local_leaf = local.resolve(local_id)
    _, target_leaf = target.resolve(target_id)
    _require(local_leaf['kind'] == target_leaf['kind'] and local_leaf['kind'] in (1, 2), 'field category differs')
    if local_leaf['kind'] == 1:
        _require(local_leaf['data'] == target_leaf['data'], 'integer field representation differs')
    return local_bits // 8, target_bits // 8


def expected_programs(policy_elf, kernel_btf, map_fds):
    """Return exact loaded instructions, attach IDs and map relocation sites.

    This fixed policy uses FIELD_BYTE_OFFSET relocations in MOV64 immediate
    instructions. Other CO-RE mechanisms fail closed instead of approximating
    the userspace loader. Target anonymous unions are traversed by member name.
    """
    _require(type(map_fds) is dict and set(map_fds) == MAPS and all(
        type(fd) is int and 0 <= fd <= 0x7fffffff for fd in map_fds.values()) and
        len(set(map_fds.values())) == len(MAPS), 'bootstrap map descriptors invalid')
    elf = _ELF(policy_elf)
    local = _BTF(elf.named['.BTF']['body'])
    target = _BTF(kernel_btf)
    programs = {name: bytearray(elf.named[name]['body']) for name in HOOKS}
    _require(all(0 < len(body) <= 4096 * 8 and len(body) % 8 == 0 for body in programs.values()),
             'program instruction extent invalid')
    ext = elf.named['.BTF.ext']['body']
    _require(len(ext) >= 32, 'BTF extension header missing')
    magic, version, flags, header, foff, flen, loff, llen, coff, clen = struct.unpack_from('<HBBIIIIIII', ext)
    _require((magic, version, flags, header) == (0xeb9f, 1, 0, 32) and
             foff == 0 and loff == flen and coff == loff + llen and header + coff + clen == len(ext),
             'BTF extension geometry invalid')
    core = _slice(ext, header + coff, clen)
    _require(len(core) >= 4 and struct.unpack_from('<I', core)[0] == 16, 'CO-RE record size differs')
    cursor = 4
    core_sites = set()
    while cursor < len(core):
        _require(cursor + 8 <= len(core), 'CO-RE section header missing')
        section_name, count = struct.unpack_from('<II', core, cursor)
        section_name = _string(local.strings, section_name)
        _require(section_name in programs and count <= 4096, 'unknown CO-RE program section')
        cursor += 8
        records = _slice(core, cursor, count * 16)
        for index in range(count):
            offset, type_id, access_string, kind = struct.unpack_from('<IIII', records, index * 16)
            _require(kind == 0 and (section_name, offset) not in core_sites, 'unsupported or duplicate CO-RE relocation')
            core_sites.add((section_name, offset))
            access_string = _string(local.strings, access_string)
            parts = access_string.split(':')
            _require(all(part and part.isascii() and part.isdecimal() and (part == '0' or part[0] != '0')
                         for part in parts), 'CO-RE access grammar invalid')
            old, new = _field_offset(local, target, type_id, [int(part) for part in parts])
            body = programs[section_name]
            insn = _slice(body, offset, 8)
            _require(offset % 8 == 0 and insn[0] == 0xb7 and insn[1] >> 4 == 0 and
                     insn[1] & 15 <= 10 and insn[2:4] == b'\0\0' and
                     struct.unpack_from('<i', insn, 4)[0] == old and new <= 0x7fffffff,
                     'CO-RE instruction does not match local field offset')
            struct.pack_into('<i', body, offset + 4, new)
        cursor += len(records)
    _require(cursor == len(core), 'CO-RE section tail invalid')
    map_sites = {name: [] for name in programs}
    symbol_table = elf.named['.symtab']['index']
    for section in elf.sections:
        if section['type'] not in (4, 9):
            continue
        _require(section['info'] < len(elf.sections), 'relocation target missing')
        program = elf.sections[section['info']]['name']
        if program not in programs:
            continue
        _require(section['type'] == 9 and section['entry_size'] == 16 and section['link'] == symbol_table and
                 len(section['body']) % 16 == 0, 'unsupported instruction relocation table')
        for cursor in range(0, len(section['body']), 16):
            offset, info = struct.unpack_from('<QQ', section['body'], cursor)
            _require(info & 0xffffffff == 1 and info >> 32 < len(elf.symbols), 'unknown instruction relocation')
            symbol = elf.symbols[info >> 32]
            _require(symbol['name'] in MAPS and symbol['section'] == elf.named['.maps']['index'], 'unknown map relocation')
            body = programs[program]
            insn = _slice(body, offset, 16)
            _require(offset % 8 == 0 and (program, offset) not in core_sites and
                     offset not in {site for site, _ in map_sites[program]} and insn[0] == 0x18 and
                     insn[1] >> 4 == 0 and insn[1] & 15 <= 10 and insn[2:] == b'\0' * 14,
                     'map relocation instruction invalid')
            body[offset + 1] |= 0x10
            struct.pack_into('<i', body, offset + 4, map_fds[symbol['name']])
            map_sites[program].append((offset, symbol['name']))
    result = {}
    for section, (hook, _) in HOOKS.items():
        result[hook] = {'bytes': bytes(programs[section]),
                        'attach_btf_id': target.unique(12, 'bpf_lsm_' + hook),
                        'map_relocations': sorted(map_sites[section])}
    return result
