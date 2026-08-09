package attachments

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestPreviewImageGoldenMetadataFreeBoundedPNG(t *testing.T) {
	t.Parallel()

	paths := previewTestWorkspace(t)
	var source bytes.Buffer
	input := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	input.Set(0, 0, color.NRGBA{R: 0xff, A: 0xff})
	input.Set(1, 0, color.NRGBA{G: 0xff, A: 0x80})
	if err := jpeg.Encode(&source, input, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("jpeg.Encode() error = %v", err)
	}
	if err := os.WriteFile(paths.source, source.Bytes(), 0o600); err != nil {
		t.Fatalf("write image source: %v", err)
	}

	processor := newPreviewProcessor(PreviewConfig{
		MaxSourceBytes: int64(source.Len()), MaxOutputBytes: 1024,
		MaxImagePixels: 4, PDFInfoBinary: "/configured/pdfinfo",
		PDFToPPMBinary: "/configured/pdftoppm",
	}, nil)
	preview, err := processor.process(context.Background(), ProcessorProfileImage, paths)
	if err != nil {
		t.Fatalf("process(image) error = %v", err)
	}
	if !preview.HasPreview || preview.MediaType != ManagedPreviewMediaTypePNG {
		t.Fatalf("process(image) = %#v", preview)
	}
	if int64(len(preview.Bytes)) > 1024 {
		t.Fatalf("PNG preview size = %d, want <= 1024", len(preview.Bytes))
	}
	if bytes.Contains(preview.Bytes, []byte("tEXt")) || bytes.Contains(preview.Bytes, []byte("eXIf")) {
		t.Fatalf("PNG preview retained metadata chunks: %x", preview.Bytes)
	}
	digest := sha256.Sum256(preview.Bytes)
	if got, want := hex.EncodeToString(digest[:]), "dac4e6f598e26f4dcfb32ea88f81375f42a14739719a9761db54160b1267ed9d"; got != want {
		t.Fatalf("PNG preview digest = %q, want golden %q", got, want)
	}
}

func TestPreviewImageAcceptsPNGAndWebP(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name    string
		content func(*testing.T) []byte
	}{
		{name: "PNG", content: func(t *testing.T) []byte { return tinyPNG(t) }},
		{name: "WebP", content: func(t *testing.T) []byte { return admissionWebP(t) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			paths := previewTestWorkspace(t)
			source := tt.content(t)
			if err := os.WriteFile(paths.source, source, 0o600); err != nil {
				t.Fatal(err)
			}
			processor := newPreviewProcessor(PreviewConfig{
				MaxSourceBytes: int64(len(source)), MaxOutputBytes: 1024, MaxImagePixels: 16,
				PDFInfoBinary: "/configured/pdfinfo", PDFToPPMBinary: "/configured/pdftoppm",
			}, nil)
			preview, err := processor.process(context.Background(), ProcessorProfileImage, paths)
			if err != nil {
				t.Fatalf("process(%s) error = %v", tt.name, err)
			}
			if !preview.HasPreview || preview.MediaType != ManagedPreviewMediaTypePNG || len(preview.Bytes) == 0 {
				t.Fatalf("process(%s) = %#v", tt.name, preview)
			}
			decoded, err := png.Decode(bytes.NewReader(preview.Bytes))
			if err != nil {
				t.Fatalf("decode %s canonical PNG: %v", tt.name, err)
			}
			if decoded.Bounds().Dx() != 1 || decoded.Bounds().Dy() != 1 {
				t.Fatalf("decode %s bounds = %v, want 1x1", tt.name, decoded.Bounds())
			}
		})
	}
}

func TestPreviewImageRejectsPNGTrailingBytes(t *testing.T) {
	t.Parallel()

	paths := previewTestWorkspace(t)
	source := append(append([]byte(nil), tinyPNG(t)...), []byte("trailing")...)
	if err := os.WriteFile(paths.source, source, 0o600); err != nil {
		t.Fatal(err)
	}
	processor := newPreviewProcessor(PreviewConfig{
		MaxSourceBytes: int64(len(source)), MaxOutputBytes: 1024, MaxImagePixels: 16,
		PDFInfoBinary: "/configured/pdfinfo", PDFToPPMBinary: "/configured/pdftoppm",
	}, nil)
	if _, err := processor.process(context.Background(), ProcessorProfileImage, paths); !errors.Is(err, ErrInvalidPreviewContent) {
		t.Fatalf("process(PNG trailing bytes) error = %v, want ErrInvalidPreviewContent", err)
	}
}

func TestPreviewImageRejectsOutputOverBound(t *testing.T) {
	t.Parallel()

	paths := previewTestWorkspace(t)
	input := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	var source bytes.Buffer
	if err := jpeg.Encode(&source, input, nil); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.source, source.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	processor := newPreviewProcessor(PreviewConfig{
		MaxSourceBytes: int64(source.Len()), MaxOutputBytes: 8, MaxImagePixels: 4,
		PDFInfoBinary: "/configured/pdfinfo", PDFToPPMBinary: "/configured/pdftoppm",
	}, nil)
	if _, err := processor.process(context.Background(), ProcessorProfileImage, paths); !errors.Is(err, ErrPreviewLimitExceeded) {
		t.Fatalf("process(oversized image) error = %v, want ErrPreviewLimitExceeded", err)
	}
}

func TestPreviewImagePixelOverflowReturnsPreviewLimit(t *testing.T) {
	t.Parallel()

	paths := previewTestWorkspace(t)
	var source bytes.Buffer
	if err := jpeg.Encode(&source, image.NewNRGBA(image.Rect(0, 0, 2, 1)), nil); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.source, source.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	processor := newPreviewProcessor(PreviewConfig{
		MaxSourceBytes: int64(source.Len()), MaxOutputBytes: 1024, MaxImagePixels: 1,
		PDFInfoBinary: "/configured/pdfinfo", PDFToPPMBinary: "/configured/pdftoppm",
	}, nil)
	if _, err := processor.process(context.Background(), ProcessorProfileImage, paths); !errors.Is(err, ErrPreviewLimitExceeded) {
		t.Fatalf("process(pixel overflow) error = %v, want ErrPreviewLimitExceeded", err)
	}
}

func TestPreviewImageCancellationAfterDecodeDoesNotReturnSuccess(t *testing.T) {
	t.Parallel()

	paths := previewTestWorkspace(t)
	var source bytes.Buffer
	if err := jpeg.Encode(&source, image.NewNRGBA(image.Rect(0, 0, 2, 1)), nil); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.source, source.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := &cancelAfterErrContext{Context: context.Background(), triggerCall: 5}
	processor := newPreviewProcessor(PreviewConfig{
		MaxSourceBytes: int64(source.Len()), MaxOutputBytes: 1024, MaxImagePixels: 4,
		PDFInfoBinary: "/configured/pdfinfo", PDFToPPMBinary: "/configured/pdftoppm",
	}, nil)
	if preview, err := processor.process(ctx, ProcessorProfileImage, paths); !errors.Is(err, context.Canceled) || preview.HasPreview {
		t.Fatalf("process(image cancellation) = %#v, %v; want no preview and context.Canceled", preview, err)
	}
}

func TestPreviewTextCancellationAfterReadDoesNotReturnSuccess(t *testing.T) {
	t.Parallel()

	paths := previewTestWorkspace(t)
	if err := os.WriteFile(paths.source, []byte("bounded text"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := &cancelAfterErrContext{Context: context.Background(), triggerCall: 3}
	processor := newPreviewProcessor(PreviewConfig{
		MaxSourceBytes: 1024, MaxOutputBytes: 1024, MaxImagePixels: 1,
		PDFInfoBinary: "/configured/pdfinfo", PDFToPPMBinary: "/configured/pdftoppm",
	}, nil)
	if preview, err := processor.process(ctx, ProcessorProfileText, paths); !errors.Is(err, context.Canceled) || preview.HasPreview {
		t.Fatalf("process(text cancellation) = %#v, %v; want no preview and context.Canceled", preview, err)
	}
}

func TestPreviewImageRejectsFormatOutsideClosedProfile(t *testing.T) {
	t.Parallel()

	paths := previewTestWorkspace(t)
	var source bytes.Buffer
	if err := gif.Encode(&source, image.NewPaletted(image.Rect(0, 0, 1, 1), color.Palette{color.Black}), nil); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.source, source.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	processor := newPreviewProcessor(PreviewConfig{
		MaxSourceBytes: int64(source.Len()), MaxOutputBytes: 1024, MaxImagePixels: 1,
		PDFInfoBinary: "/configured/pdfinfo", PDFToPPMBinary: "/configured/pdftoppm",
	}, nil)
	if _, err := processor.process(context.Background(), ProcessorProfileImage, paths); !errors.Is(err, ErrInvalidPreviewContent) {
		t.Fatalf("process(GIF image profile) error = %v, want ErrInvalidPreviewContent", err)
	}
}

func TestPreviewPDFFixedFirstPagePopplerProfile(t *testing.T) {
	t.Parallel()

	paths := previewTestWorkspace(t)
	if err := os.WriteFile(paths.source, []byte("%PDF-1.7\nfixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	type invocation struct {
		binary string
		args   []string
	}
	var invocations []invocation
	runner := func(ctx context.Context, binary string, args []string, stdout, stderr io.Writer) error {
		if stderr != io.Discard {
			t.Fatalf("Poppler command captured stderr writer %T", stderr)
		}
		invocations = append(invocations, invocation{binary: binary, args: append([]string(nil), args...)})
		if binary == "/configured/pdftoppm" {
			if stdout == io.Discard {
				t.Fatal("pdftoppm stdout was discarded")
			}
			_, err := stdout.Write(tinyPNG(t))
			return err
		}
		if stdout != io.Discard {
			t.Fatalf("pdfinfo captured stdout writer %T", stdout)
		}
		return nil
	}
	processor := newPreviewProcessor(PreviewConfig{
		MaxSourceBytes: 1024, MaxOutputBytes: 1024, MaxImagePixels: 16,
		PDFInfoBinary: "/configured/pdfinfo", PDFToPPMBinary: "/configured/pdftoppm",
	}, runner)
	preview, err := processor.process(context.Background(), ProcessorProfilePDF, paths)
	if err != nil {
		t.Fatalf("process(PDF) error = %v", err)
	}
	if !preview.HasPreview || preview.MediaType != ManagedPreviewMediaTypePNG || int64(len(preview.Bytes)) > 1024 {
		t.Fatalf("process(PDF) = %#v", preview)
	}
	if _, err := os.Lstat(paths.preview); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("command-owned Poppler output residue: %v", err)
	}
	for _, name := range []string{processorWorkspaceCacheName, processorWorkspaceConfigName, processorWorkspaceTempName} {
		info, err := os.Stat(filepath.Join(paths.workspace, name))
		if err != nil {
			t.Fatalf("stat private Poppler %s directory: %v", name, err)
		}
		if !info.IsDir() || info.Mode().Perm() != 0o700 {
			t.Fatalf("private Poppler %s mode = %v, want directory 700", name, info.Mode())
		}
	}
	fontConfigInfo, err := os.Stat(filepath.Join(paths.workspace, processorWorkspaceFontConfigName))
	if err != nil {
		t.Fatalf("stat private Poppler font config: %v", err)
	}
	if !fontConfigInfo.Mode().IsRegular() || fontConfigInfo.Mode().Perm() != 0o600 {
		t.Fatalf("private Poppler font config mode = %v, want regular 600", fontConfigInfo.Mode())
	}
	want := []invocation{
		{binary: "/configured/pdfinfo", args: []string{paths.source}},
		{binary: "/configured/pdftoppm", args: []string{
			"-f", "1", "-l", "1", "-singlefile", "-png", "-scale-to", "2048",
			paths.source,
		}},
	}
	if !reflect.DeepEqual(invocations, want) {
		t.Fatalf("Poppler invocations = %#v, want %#v", invocations, want)
	}
}

func TestPreviewPDFRejectsTruncatedAndTrailingPNG(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name   string
		mutate func([]byte) []byte
	}{
		{name: "truncated", mutate: func(value []byte) []byte {
			return value[:len(value)-1]
		}},
		{name: "trailing bytes", mutate: func(value []byte) []byte {
			return append(append([]byte(nil), value...), []byte("trailing")...)
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			paths := previewTestWorkspace(t)
			if err := os.WriteFile(paths.source, []byte("%PDF-1.7\nfixture"), 0o600); err != nil {
				t.Fatal(err)
			}
			pngBytes := tt.mutate(tinyPNG(t))
			runner := func(_ context.Context, binary string, _ []string, stdout, _ io.Writer) error {
				if binary == "/configured/pdftoppm" {
					_, err := stdout.Write(pngBytes)
					return err
				}
				return nil
			}
			processor := newPreviewProcessor(PreviewConfig{
				MaxSourceBytes: 1024, MaxOutputBytes: 1024, MaxImagePixels: 16,
				PDFInfoBinary: "/configured/pdfinfo", PDFToPPMBinary: "/configured/pdftoppm",
			}, runner)
			if _, err := processor.process(context.Background(), ProcessorProfilePDF, paths); !errors.Is(err, ErrInvalidPreviewContent) {
				t.Fatalf("process(%s) error = %v, want ErrInvalidPreviewContent", tt.name, err)
			}
		})
	}
}

func TestPreviewPDFCanonicalizesPNGAndStripsMetadata(t *testing.T) {
	t.Parallel()

	paths := previewTestWorkspace(t)
	if err := os.WriteFile(paths.source, []byte("%PDF-1.7\nfixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	metadataPNG := pngWithTextChunk(t, tinyPNG(t), "Comment", "processor-private")
	runner := func(_ context.Context, binary string, _ []string, stdout, _ io.Writer) error {
		if binary == "/configured/pdftoppm" {
			_, err := stdout.Write(metadataPNG)
			return err
		}
		return nil
	}
	processor := newPreviewProcessor(PreviewConfig{
		MaxSourceBytes: 1024, MaxOutputBytes: 1024, MaxImagePixels: 16,
		PDFInfoBinary: "/configured/pdfinfo", PDFToPPMBinary: "/configured/pdftoppm",
	}, runner)
	preview, err := processor.process(context.Background(), ProcessorProfilePDF, paths)
	if err != nil {
		t.Fatalf("process(PDF metadata) error = %v", err)
	}
	if bytes.Contains(preview.Bytes, []byte("tEXt")) || bytes.Contains(preview.Bytes, []byte("processor-private")) {
		t.Fatalf("canonical PNG retained metadata: %x", preview.Bytes)
	}
	decoded, err := png.Decode(bytes.NewReader(preview.Bytes))
	if err != nil {
		t.Fatalf("decode canonical PNG: %v", err)
	}
	if decoded.Bounds().Dx() != 1 || decoded.Bounds().Dy() != 1 {
		t.Fatalf("canonical PNG bounds = %v, want 1x1", decoded.Bounds())
	}
}

func TestPreviewPDFRejectsStdoutOverBound(t *testing.T) {
	t.Parallel()

	paths := previewTestWorkspace(t)
	if err := os.WriteFile(paths.source, []byte("%PDF-1.7\nfixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	pngBytes := tinyPNG(t)
	runner := func(_ context.Context, binary string, _ []string, stdout, _ io.Writer) error {
		if binary != "/configured/pdftoppm" {
			return nil
		}
		oversized := append(append([]byte(nil), pngBytes...), bytes.Repeat([]byte{'x'}, 1024)...)
		written, err := stdout.Write(oversized)
		if err != nil {
			return err
		}
		if written != len(oversized) {
			return io.ErrShortWrite
		}
		return nil
	}
	processor := newPreviewProcessor(PreviewConfig{
		MaxSourceBytes: 1024, MaxOutputBytes: int64(len(pngBytes)), MaxImagePixels: 16,
		PDFInfoBinary: "/configured/pdfinfo", PDFToPPMBinary: "/configured/pdftoppm",
	}, runner)
	if _, err := processor.process(context.Background(), ProcessorProfilePDF, paths); !errors.Is(err, ErrPreviewLimitExceeded) {
		t.Fatalf("process(oversized PDF stdout) error = %v, want ErrPreviewLimitExceeded", err)
	}
}

func TestPreviewPDFRejectsOversizedSourceBeforeCommand(t *testing.T) {
	t.Parallel()

	paths := previewTestWorkspace(t)
	if err := os.WriteFile(paths.source, []byte("%PDF-1.7\noversized"), 0o600); err != nil {
		t.Fatal(err)
	}
	commands := 0
	processor := newPreviewProcessor(PreviewConfig{
		MaxSourceBytes: 4, MaxOutputBytes: 1024, MaxImagePixels: 1,
		PDFInfoBinary: "/configured/pdfinfo", PDFToPPMBinary: "/configured/pdftoppm",
	}, func(context.Context, string, []string, io.Writer, io.Writer) error {
		commands++
		return nil
	})
	if _, err := processor.process(context.Background(), ProcessorProfilePDF, paths); !errors.Is(err, ErrPreviewLimitExceeded) {
		t.Fatalf("process(oversized PDF) error = %v, want ErrPreviewLimitExceeded", err)
	}
	if commands != 0 {
		t.Fatalf("oversized PDF ran %d commands", commands)
	}
}

func TestPreviewTextBoundedUTF8WithoutSplit(t *testing.T) {
	t.Parallel()

	paths := previewTestWorkspace(t)
	if err := os.WriteFile(paths.source, []byte("ab世cd"), 0o600); err != nil {
		t.Fatal(err)
	}
	processor := newPreviewProcessor(PreviewConfig{
		MaxSourceBytes: 32, MaxOutputBytes: 5, MaxImagePixels: 1,
		PDFInfoBinary: "/configured/pdfinfo", PDFToPPMBinary: "/configured/pdftoppm",
	}, nil)
	preview, err := processor.process(context.Background(), ProcessorProfileText, paths)
	if err != nil {
		t.Fatalf("process(text) error = %v", err)
	}
	if got, want := string(preview.Bytes), "ab世"; got != want {
		t.Fatalf("text preview = %q, want %q", got, want)
	}

	if err := os.WriteFile(paths.source, []byte{'a', 0xff}, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := processor.process(context.Background(), ProcessorProfileText, paths); !errors.Is(err, ErrInvalidPreviewContent) {
		t.Fatalf("process(invalid UTF-8) error = %v, want ErrInvalidPreviewContent", err)
	}
}

func TestPreviewRejectsSymlinkContentPathsWithoutFollowing(t *testing.T) {
	t.Parallel()

	t.Run("source", func(t *testing.T) {
		paths := previewTestWorkspace(t)
		target := filepath.Join(t.TempDir(), "target.txt")
		if err := os.WriteFile(target, []byte("secret"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, paths.source); err != nil {
			t.Fatal(err)
		}
		processor := newPreviewProcessor(PreviewConfig{
			MaxSourceBytes: 32, MaxOutputBytes: 32, MaxImagePixels: 1,
			PDFInfoBinary: "/configured/pdfinfo", PDFToPPMBinary: "/configured/pdftoppm",
		}, nil)
		if _, err := processor.process(context.Background(), ProcessorProfileText, paths); !errors.Is(err, ErrInvalidPreviewContent) {
			t.Fatalf("process(symlink source) error = %v, want ErrInvalidPreviewContent", err)
		}
	})

	t.Run("PDF output", func(t *testing.T) {
		paths := previewTestWorkspace(t)
		if err := os.WriteFile(paths.source, []byte("%PDF-1.7\nfixture"), 0o600); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(t.TempDir(), "target.png")
		original := []byte("do not overwrite")
		if err := os.WriteFile(target, original, 0o600); err != nil {
			t.Fatal(err)
		}
		runner := func(_ context.Context, binary string, _ []string, _, _ io.Writer) error {
			if binary == "/configured/pdftoppm" {
				return os.Symlink(target, paths.preview)
			}
			return nil
		}
		processor := newPreviewProcessor(PreviewConfig{
			MaxSourceBytes: 32, MaxOutputBytes: 32, MaxImagePixels: 1,
			PDFInfoBinary: "/configured/pdfinfo", PDFToPPMBinary: "/configured/pdftoppm",
		}, runner)
		if _, err := processor.process(context.Background(), ProcessorProfilePDF, paths); !errors.Is(err, ErrInvalidPreviewContent) {
			t.Fatalf("process(symlink PDF output) error = %v, want ErrInvalidPreviewContent", err)
		}
		got, err := os.ReadFile(target)
		if err != nil || !bytes.Equal(got, original) {
			t.Fatalf("symlink target = %q error=%v, want unchanged %q", got, err, original)
		}
	})
}

func TestPreviewPDFRejectsFontConfigSymlinkWithoutFollowing(t *testing.T) {
	t.Parallel()

	paths := previewTestWorkspace(t)
	if err := os.WriteFile(paths.source, []byte("%PDF-1.7\nfixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "external-fonts.conf")
	sentinel := []byte("external sentinel must remain unchanged")
	if err := os.WriteFile(target, sentinel, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(paths.workspace, processorWorkspaceFontConfigName)); err != nil {
		t.Fatal(err)
	}
	commands := 0
	processor := newPreviewProcessor(PreviewConfig{
		MaxSourceBytes: 32, MaxOutputBytes: 1024, MaxImagePixels: 1,
		PDFInfoBinary: "/configured/pdfinfo", PDFToPPMBinary: "/configured/pdftoppm",
	}, func(context.Context, string, []string, io.Writer, io.Writer) error {
		commands++
		return nil
	})
	if _, err := processor.process(context.Background(), ProcessorProfilePDF, paths); !errors.Is(err, ErrInvalidPreviewContent) {
		t.Fatalf("process(font config symlink) error = %v, want ErrInvalidPreviewContent", err)
	}
	if commands != 0 {
		t.Fatalf("font config symlink ran %d commands", commands)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read external font config sentinel: %v", err)
	}
	if !bytes.Equal(got, sentinel) {
		t.Fatalf("external font config sentinel = %q, want unchanged %q", got, sentinel)
	}
}

func TestPreviewPDFRejectsPrivateCommandDirectorySymlinksWithoutFollowing(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name  string
		setup func(*testing.T, processorWorkspacePaths, string)
	}{
		{
			name: "command root",
			setup: func(t *testing.T, paths processorWorkspacePaths, target string) {
				t.Helper()
				if err := os.Symlink(target, filepath.Join(paths.workspace, previewCommandRootDirectoryName)); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "command cwd",
			setup: func(t *testing.T, paths processorWorkspacePaths, target string) {
				t.Helper()
				root := filepath.Join(paths.workspace, previewCommandRootDirectoryName)
				if err := os.Mkdir(root, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, previewCommandWorkingDirectory(paths.workspace)); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			paths := previewTestWorkspace(t)
			if err := os.WriteFile(paths.source, []byte("%PDF-1.7\nfixture"), 0o600); err != nil {
				t.Fatal(err)
			}
			target := t.TempDir()
			test.setup(t, paths, target)
			commands := 0
			processor := newPreviewProcessor(PreviewConfig{
				MaxSourceBytes: 32, MaxOutputBytes: 1024, MaxImagePixels: 1,
				PDFInfoBinary: "/configured/pdfinfo", PDFToPPMBinary: "/configured/pdftoppm",
			}, func(context.Context, string, []string, io.Writer, io.Writer) error {
				commands++
				return nil
			})
			if _, err := processor.process(context.Background(), ProcessorProfilePDF, paths); !errors.Is(err, ErrInvalidPreviewContent) {
				t.Fatalf("process(private command symlink) error = %v, want ErrInvalidPreviewContent", err)
			}
			if commands != 0 {
				t.Fatalf("private command symlink ran %d commands", commands)
			}
			entries, err := os.ReadDir(target)
			if err != nil || len(entries) != 0 {
				t.Fatalf("private command symlink target entries = %v error=%v, want empty", entries, err)
			}
		})
	}
}

func TestPreviewPDFRejectsPreexistingOutputSymlinkBeforeCommand(t *testing.T) {
	t.Parallel()

	paths := previewTestWorkspace(t)
	if err := os.WriteFile(paths.source, []byte("%PDF-1.7\nfixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "external-preview.png")
	sentinel := []byte("external preview sentinel must remain unchanged")
	if err := os.WriteFile(target, sentinel, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, paths.preview); err != nil {
		t.Fatal(err)
	}
	commands := 0
	processor := newPreviewProcessor(PreviewConfig{
		MaxSourceBytes: 32, MaxOutputBytes: 1024, MaxImagePixels: 1,
		PDFInfoBinary: "/configured/pdfinfo", PDFToPPMBinary: "/configured/pdftoppm",
	}, func(_ context.Context, binary string, _ []string, _, _ io.Writer) error {
		commands++
		if binary == "/configured/pdftoppm" {
			return os.WriteFile(paths.preview, tinyPNG(t), 0o600)
		}
		return nil
	})
	if _, err := processor.process(context.Background(), ProcessorProfilePDF, paths); !errors.Is(err, ErrInvalidPreviewContent) {
		t.Fatalf("process(preexisting PDF output symlink) error = %v, want ErrInvalidPreviewContent", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read external preview sentinel: %v", err)
	}
	if !bytes.Equal(got, sentinel) {
		t.Fatalf("external preview sentinel = %q, want unchanged %q", got, sentinel)
	}
	if commands != 0 {
		t.Fatalf("preexisting PDF output symlink ran %d commands", commands)
	}
}

func TestPreviewArchiveHasNoPreviewAndRunsNoCommand(t *testing.T) {
	t.Parallel()

	paths := previewTestWorkspace(t)
	commands := 0
	processor := newPreviewProcessor(PreviewConfig{
		MaxSourceBytes: 32, MaxOutputBytes: 32, MaxImagePixels: 1,
		PDFInfoBinary: "/configured/pdfinfo", PDFToPPMBinary: "/configured/pdftoppm",
	}, func(context.Context, string, []string, io.Writer, io.Writer) error {
		commands++
		return nil
	})
	preview, err := processor.process(context.Background(), ProcessorProfileArchive, paths)
	if err != nil {
		t.Fatalf("process(archive) error = %v", err)
	}
	if preview.HasPreview || preview.MediaType != "" || len(preview.Bytes) != 0 || commands != 0 {
		t.Fatalf("process(archive) = %#v, commands = %d", preview, commands)
	}
}

func TestPreviewRequiresAbsoluteConfiguredPopplerBinaryPaths(t *testing.T) {
	t.Parallel()

	for _, config := range []PreviewConfig{
		{MaxSourceBytes: 1, MaxOutputBytes: 1, MaxImagePixels: 1, PDFInfoBinary: "pdfinfo", PDFToPPMBinary: "/configured/pdftoppm"},
		{MaxSourceBytes: 1, MaxOutputBytes: 1, MaxImagePixels: 1, PDFInfoBinary: "/configured/pdfinfo", PDFToPPMBinary: "pdftoppm"},
	} {
		if _, err := NewPreviewProcessor(config); !errors.Is(err, ErrInvalidPreviewConfig) {
			t.Fatalf("NewPreviewProcessor(relative binary) error = %v, want ErrInvalidPreviewConfig", err)
		}
	}
}

func TestPreviewPopplerEnvironmentIsPrivateAndClosed(t *testing.T) {
	t.Parallel()

	workspace := filepath.Join(t.TempDir(), "cpw_environment1")
	environment := previewCommandEnvironment(workspace)
	got := make(map[string]string, len(environment))
	for _, entry := range environment {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || key == "" {
			t.Fatalf("invalid Poppler environment entry %q", entry)
		}
		got[key] = value
	}
	want := map[string]string{
		"FONTCONFIG_FILE": filepath.Join(workspace, processorWorkspaceFontConfigName),
		"FONTCONFIG_PATH": filepath.Join(workspace, processorWorkspaceConfigName),
		"HOME":            workspace,
		"LANG":            "C",
		"LC_ALL":          "C",
		"XDG_CACHE_HOME":  filepath.Join(workspace, processorWorkspaceCacheName),
		"XDG_CONFIG_HOME": filepath.Join(workspace, processorWorkspaceConfigName),
		"TMPDIR":          filepath.Join(workspace, processorWorkspaceTempName),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Poppler environment = %#v, want private closed %#v", got, want)
	}
}

func TestPreviewPopplerWrapperRelativeCacheStaysInsideWorkspace(t *testing.T) {
	paths := previewTestWorkspace(t)
	if err := os.WriteFile(paths.source, []byte("%PDF-1.7\nfixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	markerDigest := sha256.Sum256([]byte(paths.workspace))
	marker := "houfeng-preview-command-cwd-" + hex.EncodeToString(markerDigest[:8])
	relativeMarker := filepath.Join("..", "..", "var", "cache", "fontconfig", marker)
	packageCWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("get package working directory: %v", err)
	}
	sharedMarker := filepath.Clean(filepath.Join(packageCWD, relativeMarker))
	if _, err := os.Lstat(sharedMarker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("shared cache marker precondition = %v, want absent %s", err, sharedMarker)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(sharedMarker); err != nil {
			t.Errorf("remove test-owned shared cache marker: %v", err)
		}
		for path := filepath.Dir(sharedMarker); path != filepath.Clean(filepath.Join(packageCWD, "..", "..")); path = filepath.Dir(path) {
			_ = os.Remove(path)
		}
	})

	pngPath := filepath.Join(t.TempDir(), "preview.png")
	if err := os.WriteFile(pngPath, tinyPNG(t), 0o600); err != nil {
		t.Fatal(err)
	}
	baseScript := "#!/bin/sh\nset -eu\n/usr/bin/mkdir -p " + relativeMarker +
		"\n/usr/bin/printf cache > " + filepath.Join(relativeMarker, "cache") + "\n"
	writeWrapper := func(name, script string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
			t.Fatalf("write %s wrapper: %v", name, err)
		}
		return path
	}
	pdfInfo := writeWrapper("pdfinfo", baseScript)
	pdfToPPM := writeWrapper("pdftoppm", baseScript+"/usr/bin/cat "+strconv.Quote(pngPath)+"\n")
	processor := newPreviewProcessor(PreviewConfig{
		MaxSourceBytes: 1024, MaxOutputBytes: 1024, MaxImagePixels: 16,
		PDFInfoBinary: pdfInfo, PDFToPPMBinary: pdfToPPM,
	}, nil)
	preview, err := processor.process(context.Background(), ProcessorProfilePDF, paths)
	if err != nil {
		t.Fatalf("process(wrapper PDF) error = %v", err)
	}
	if !preview.HasPreview || preview.MediaType != ManagedPreviewMediaTypePNG {
		t.Fatalf("process(wrapper PDF) = %#v", preview)
	}
	if _, err := os.Lstat(sharedMarker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("wrapper created shared cache residue outside workspace: %s error=%v", sharedMarker, err)
	}
	privateMarker := filepath.Join(paths.workspace, "var", "cache", "fontconfig", marker)
	if info, err := os.Stat(privateMarker); err != nil || !info.IsDir() {
		t.Fatalf("wrapper private cache marker = %v error=%v, want directory %s", info, err, privateMarker)
	}
}

func TestPreviewRealPopplerProfile(t *testing.T) {
	if os.Getenv("HOUFENG_POPPLER_INTEGRATION") != "1" {
		t.Skip("set HOUFENG_POPPLER_INTEGRATION=1 to run the real Poppler profile")
	}
	pdfInfo := os.Getenv("HOUFENG_PDFINFO_BINARY")
	if pdfInfo == "" {
		pdfInfo, _ = exec.LookPath("pdfinfo")
	}
	pdfToPPM := os.Getenv("HOUFENG_PDFTOPPM_BINARY")
	if pdfToPPM == "" {
		pdfToPPM, _ = exec.LookPath("pdftoppm")
	}
	if pdfInfo == "" || pdfToPPM == "" {
		t.Skip("configured pdfinfo and pdftoppm binaries are required")
	}
	fixture := os.Getenv("HOUFENG_POPPLER_PDF_FIXTURE")
	fixtureInfo, err := os.Stat(fixture)
	if fixture == "" || err != nil || !fixtureInfo.Mode().IsRegular() {
		t.Skip("HOUFENG_POPPLER_PDF_FIXTURE must name a regular PDF document")
	}
	content, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatalf("read Poppler fixture: %v", err)
	}
	packageCWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("get package working directory: %v", err)
	}
	sharedCacheRoot := filepath.Clean(filepath.Join(packageCWD, "..", "..", "var"))
	if _, err := os.Lstat(sharedCacheRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("real Poppler shared-cache precondition = %v, want absent %s", err, sharedCacheRoot)
	}
	paths := previewTestWorkspace(t)
	if err := os.WriteFile(paths.source, content, 0o600); err != nil {
		t.Fatalf("materialize Poppler fixture: %v", err)
	}
	processor := newPreviewProcessor(PreviewConfig{
		MaxSourceBytes: int64(len(content)), MaxOutputBytes: 16 * MiB,
		MaxImagePixels: 2048 * 2048, PDFInfoBinary: pdfInfo, PDFToPPMBinary: pdfToPPM,
	}, nil)
	preview, err := processor.process(context.Background(), ProcessorProfilePDF, paths)
	if err != nil {
		t.Fatalf("process(real Poppler PDF) error = %v", err)
	}
	if !preview.HasPreview || preview.MediaType != ManagedPreviewMediaTypePNG ||
		len(preview.Bytes) == 0 || int64(len(preview.Bytes)) > 16*MiB {
		t.Fatalf("real Poppler preview = %#v", preview)
	}
	if _, err := os.Lstat(sharedCacheRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("real Poppler created repository/shared cache residue at %s: %v", sharedCacheRoot, err)
	}
	commandCWD := previewCommandWorkingDirectory(paths.workspace)
	commandInfo, err := os.Stat(commandCWD)
	if err != nil || !commandInfo.IsDir() || commandInfo.Mode().Perm() != 0o700 {
		t.Fatalf("real Poppler private command cwd mode = %v error=%v, want directory 700", commandInfo, err)
	}
	generatedCacheFile := false
	if err := filepath.WalkDir(paths.workspace, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(paths.workspace, path)
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.Contains(string(filepath.Separator)+relative+string(filepath.Separator),
			string(filepath.Separator)+"cache"+string(filepath.Separator)) {
			generatedCacheFile = true
		}
		return nil
	}); err != nil {
		t.Fatalf("walk real Poppler private workspace: %v", err)
	}
	if !generatedCacheFile {
		t.Fatalf("real Poppler %s generated no cache file inside private workspace %s", pdfToPPM, paths.workspace)
	}
}

func previewTestWorkspace(t *testing.T) processorWorkspacePaths {
	t.Helper()
	root := t.TempDir()
	paths, err := deriveProcessorWorkspacePaths(root, "cpw_preview1")
	if err != nil {
		t.Fatalf("deriveProcessorWorkspacePaths() error = %v", err)
	}
	if err := os.Mkdir(paths.workspace, 0o700); err != nil {
		t.Fatalf("mkdir preview workspace: %v", err)
	}
	return paths
}

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	if err := png.Encode(&output, image.NewNRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatalf("png.Encode() error = %v", err)
	}
	return output.Bytes()
}

func pngWithTextChunk(t *testing.T, base []byte, keyword, value string) []byte {
	t.Helper()
	for offset := 8; offset+12 <= len(base); {
		length := int(binary.BigEndian.Uint32(base[offset : offset+4]))
		end := offset + 12 + length
		if length < 0 || end > len(base) {
			t.Fatalf("invalid PNG fixture chunk at offset %d", offset)
		}
		if string(base[offset+4:offset+8]) == "IEND" {
			data := append([]byte(keyword+"\x00"), []byte(value)...)
			chunk := make([]byte, 12+len(data))
			binary.BigEndian.PutUint32(chunk[:4], uint32(len(data)))
			copy(chunk[4:8], []byte("tEXt"))
			copy(chunk[8:8+len(data)], data)
			binary.BigEndian.PutUint32(chunk[8+len(data):], crc32.ChecksumIEEE(chunk[4:8+len(data)]))
			result := make([]byte, 0, len(base)+len(chunk))
			result = append(result, base[:offset]...)
			result = append(result, chunk...)
			result = append(result, base[offset:]...)
			return result
		}
		offset = end
	}
	t.Fatal("PNG fixture has no IEND chunk")
	return nil
}

type cancelAfterErrContext struct {
	context.Context
	triggerCall int
	calls       int
	canceled    bool
}

func (ctx *cancelAfterErrContext) Err() error {
	ctx.calls++
	if ctx.canceled {
		return context.Canceled
	}
	if ctx.calls >= ctx.triggerCall {
		ctx.canceled = true
		return nil
	}
	return nil
}
