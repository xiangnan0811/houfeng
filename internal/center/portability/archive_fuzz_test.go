package portability

import "testing"

func FuzzReadArchiveV1(f *testing.F) {
	valid, err := WriteArchiveV1([]ArchiveEntry{{
		Path: "records/rec_a/document.md", Classification: ArchiveClassMarkdown, Payload: []byte("# seed\n"),
	}})
	if err != nil {
		f.Fatalf("WriteArchiveV1() error = %v", err)
	}
	f.Add(valid)
	f.Add(valid[:len(valid)/3])
	f.Add([]byte{'P', 'K', 0x03, 0x04, 0x00})
	f.Add([]byte("../escape"))

	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 8192 {
			t.Skip()
		}
		manifest, entries, err := ReadArchiveV1(raw)
		if err != nil {
			if !isInvalidArchive(err) {
				t.Fatalf("ReadArchiveV1() error = %v, want ErrInvalidArchive or success", err)
			}
			return
		}
		if manifest.Format != ArchiveFormatV1 || len(entries) == 0 {
			t.Fatalf("accepted archive without v1 membership: %#v %d", manifest, len(entries))
		}
	})
}
