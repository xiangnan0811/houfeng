package attachments

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"unicode/utf8"

	_ "golang.org/x/image/webp"
)

const (
	previewCommandRootDirectoryName    = "command"
	previewCommandWorkingDirectoryName = "cwd"
)

var (
	ErrInvalidPreviewConfig  = errors.New("invalid attachment preview configuration")
	ErrInvalidPreviewContent = errors.New("invalid attachment preview content")
	ErrPreviewLimitExceeded  = errors.New("attachment preview limit exceeded")
)

type PreviewConfig struct {
	MaxSourceBytes int64
	MaxOutputBytes int64
	MaxImagePixels int64
	PDFInfoBinary  string
	PDFToPPMBinary string
}

func (config PreviewConfig) validate() error {
	if config.MaxSourceBytes <= 0 || config.MaxOutputBytes <= 0 || config.MaxImagePixels <= 0 ||
		!filepath.IsAbs(config.PDFInfoBinary) || !filepath.IsAbs(config.PDFToPPMBinary) {
		return ErrInvalidPreviewConfig
	}
	return nil
}

type PreviewArtifact struct {
	HasPreview bool
	MediaType  string
	Bytes      []byte
}

type previewCommandRunner func(context.Context, string, []string, io.Writer, io.Writer) error
type securePreviewCommandRunner func(context.Context, string, []string, secureProcessorWorkspace, io.Writer, io.Writer) error

type PreviewProcessor struct {
	config    PreviewConfig
	run       previewCommandRunner
	runSecure securePreviewCommandRunner
}

func NewPreviewProcessor(config PreviewConfig) (*PreviewProcessor, error) {
	if config.validate() != nil {
		return nil, ErrInvalidPreviewConfig
	}
	return newPreviewProcessor(config, nil), nil
}

func newPreviewProcessor(config PreviewConfig, runner previewCommandRunner) *PreviewProcessor {
	if runner == nil {
		runner = runPreviewCommand
	}
	return &PreviewProcessor{config: config, run: runner, runSecure: runPreviewCommandSecure}
}

func (processor *PreviewProcessor) process(
	ctx context.Context,
	profile ProcessorProfile,
	paths processorWorkspacePaths,
) (PreviewArtifact, error) {
	if ctx == nil || processor == nil || processor.config.validate() != nil || processor.run == nil || processor.runSecure == nil {
		return PreviewArtifact{}, ErrInvalidPreviewConfig
	}
	if err := ctx.Err(); err != nil {
		return PreviewArtifact{}, err
	}
	switch profile {
	case ProcessorProfileImage:
		return processor.processImage(ctx, paths)
	case ProcessorProfilePDF:
		return processor.processPDF(ctx, paths)
	case ProcessorProfileText:
		return processor.processText(ctx, paths)
	case ProcessorProfileArchive:
		return PreviewArtifact{}, nil
	default:
		return PreviewArtifact{}, ErrInvalidPreviewContent
	}
}

func (processor *PreviewProcessor) processImage(
	ctx context.Context,
	paths processorWorkspacePaths,
) (PreviewArtifact, error) {
	var source []byte
	var err error
	if paths.secure != nil {
		source, err = paths.secure.readSourceBounded(ctx, processor.config.MaxSourceBytes)
	} else {
		source, err = readRegularFileBounded(ctx, paths.source, processor.config.MaxSourceBytes)
	}
	if err != nil {
		return PreviewArtifact{}, err
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(source))
	if err != nil || !knownPreviewImageFormat(format) || config.Width <= 0 || config.Height <= 0 {
		return PreviewArtifact{}, ErrInvalidPreviewContent
	}
	if int64(config.Width) > processor.config.MaxImagePixels/int64(config.Height) {
		return PreviewArtifact{}, ErrPreviewLimitExceeded
	}
	switch format {
	case "png":
		if err := validateExactPNGStream(ctx, source); err != nil {
			if contextErr := ctx.Err(); contextErr != nil {
				return PreviewArtifact{}, contextErr
			}
			return PreviewArtifact{}, ErrInvalidPreviewContent
		}
	case "jpeg":
		if !hasExactJPEGStream(source) {
			return PreviewArtifact{}, ErrInvalidPreviewContent
		}
	case "webp":
		if !hasExactWebPContainer(source) {
			return PreviewArtifact{}, ErrInvalidPreviewContent
		}
	}
	if int64(config.Width)*int64(config.Height) > processor.config.MaxImagePixels {
		return PreviewArtifact{}, ErrPreviewLimitExceeded
	}
	decoded, _, err := image.Decode(bytes.NewReader(source))
	if err != nil {
		return PreviewArtifact{}, ErrInvalidPreviewContent
	}
	if err := ctx.Err(); err != nil {
		return PreviewArtifact{}, err
	}
	output := &boundedPreviewBuffer{limit: processor.config.MaxOutputBytes}
	if err := png.Encode(output, decoded); err != nil {
		return PreviewArtifact{}, ErrInvalidPreviewContent
	}
	if output.exceeded {
		return PreviewArtifact{}, ErrPreviewLimitExceeded
	}
	if err := ctx.Err(); err != nil {
		return PreviewArtifact{}, err
	}
	return PreviewArtifact{
		HasPreview: true, MediaType: ManagedPreviewMediaTypePNG,
		Bytes: append([]byte(nil), output.Bytes()...),
	}, nil
}

func knownPreviewImageFormat(format string) bool {
	return format == "png" || format == "jpeg" || format == "webp"
}

func (processor *PreviewProcessor) processText(
	ctx context.Context,
	paths processorWorkspacePaths,
) (PreviewArtifact, error) {
	var source []byte
	var err error
	if paths.secure != nil {
		source, err = paths.secure.readSourceBounded(ctx, processor.config.MaxSourceBytes)
	} else {
		source, err = readRegularFileBounded(ctx, paths.source, processor.config.MaxSourceBytes)
	}
	if err != nil {
		return PreviewArtifact{}, err
	}
	if !utf8.Valid(source) {
		return PreviewArtifact{}, ErrInvalidPreviewContent
	}
	limit := processor.config.MaxOutputBytes
	end := len(source)
	if int64(end) > limit {
		end = int(limit)
		for end > 0 && !utf8.Valid(source[:end]) {
			end--
		}
	}
	if err := ctx.Err(); err != nil {
		return PreviewArtifact{}, err
	}
	return PreviewArtifact{
		HasPreview: true, MediaType: ManagedPreviewMediaTypeTextUTF8,
		Bytes: append([]byte(nil), source[:end]...),
	}, nil
}

func (processor *PreviewProcessor) processPDF(
	ctx context.Context,
	paths processorWorkspacePaths,
) (PreviewArtifact, error) {
	if paths.secure != nil {
		size, err := paths.secure.sourceSize(ctx)
		if err != nil {
			return PreviewArtifact{}, err
		}
		if size > processor.config.MaxSourceBytes {
			return PreviewArtifact{}, ErrPreviewLimitExceeded
		}
		if err := paths.secure.ensurePreviewAbsent(ctx); err != nil {
			return PreviewArtifact{}, err
		}
	} else {
		if err := validateRegularFileSize(paths.source, processor.config.MaxSourceBytes); err != nil {
			return PreviewArtifact{}, err
		}
		if _, err := os.Lstat(paths.preview); err == nil || !errors.Is(err, os.ErrNotExist) {
			return PreviewArtifact{}, ErrInvalidPreviewContent
		}
	}
	if paths.secure != nil {
		if err := paths.secure.preparePreviewDirectories(ctx, processor.config); err != nil {
			return PreviewArtifact{}, err
		}
	} else if err := preparePrivatePreviewCommandDirectories(paths); err != nil {
		return PreviewArtifact{}, err
	}
	sourcePath := paths.source
	if paths.secure != nil {
		sourcePath = paths.secure.commandSourcePath()
	}
	commandRun := processor.run
	secureCommandRun := processor.runSecure
	if paths.secure != nil {
		if err := secureCommandRun(ctx, processor.config.PDFInfoBinary, []string{sourcePath}, paths.secure, io.Discard, io.Discard); err != nil {
			if contextError := ctx.Err(); contextError != nil {
				return PreviewArtifact{}, contextError
			}
			return PreviewArtifact{}, ErrInvalidPreviewContent
		}
	} else if err := commandRun(ctx, processor.config.PDFInfoBinary, []string{sourcePath}, io.Discard, io.Discard); err != nil {
		if contextError := ctx.Err(); contextError != nil {
			return PreviewArtifact{}, contextError
		}
		return PreviewArtifact{}, ErrInvalidPreviewContent
	}
	arguments := []string{
		"-f", "1", "-l", "1", "-singlefile", "-png", "-scale-to", "2048",
		sourcePath,
	}
	output := &boundedPreviewBuffer{limit: processor.config.MaxOutputBytes}
	var commandErr error
	if paths.secure != nil {
		commandErr = secureCommandRun(ctx, processor.config.PDFToPPMBinary, arguments, paths.secure, output, io.Discard)
	} else {
		commandErr = commandRun(ctx, processor.config.PDFToPPMBinary, arguments, output, io.Discard)
	}
	if commandErr != nil {
		if contextError := ctx.Err(); contextError != nil {
			return PreviewArtifact{}, contextError
		}
		return PreviewArtifact{}, ErrInvalidPreviewContent
	}
	if output.exceeded {
		return PreviewArtifact{}, ErrPreviewLimitExceeded
	}
	canonical, err := canonicalizePNGPreview(ctx, output.Bytes(), processor.config.MaxImagePixels, processor.config.MaxOutputBytes)
	if err != nil {
		return PreviewArtifact{}, err
	}
	return PreviewArtifact{
		HasPreview: true, MediaType: ManagedPreviewMediaTypePNG,
		Bytes: canonical,
	}, nil
}

func canonicalizePNGPreview(ctx context.Context, content []byte, maxPixels, maxOutputBytes int64) ([]byte, error) {
	if ctx == nil || maxPixels <= 0 || maxOutputBytes <= 0 {
		return nil, ErrInvalidPreviewConfig
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateExactPNGStream(ctx, content); err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return nil, contextErr
		}
		return nil, ErrInvalidPreviewContent
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil || format != "png" || config.Width <= 0 || config.Height <= 0 {
		return nil, ErrInvalidPreviewContent
	}
	if int64(config.Width) > maxPixels/int64(config.Height) {
		return nil, ErrPreviewLimitExceeded
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	decoded, decodedFormat, err := image.Decode(bytes.NewReader(content))
	if err != nil || decodedFormat != "png" || decoded.Bounds().Dx() != config.Width ||
		decoded.Bounds().Dy() != config.Height {
		return nil, ErrInvalidPreviewContent
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	output := &boundedPreviewBuffer{limit: maxOutputBytes}
	if err := png.Encode(output, decoded); err != nil {
		return nil, ErrInvalidPreviewContent
	}
	if output.exceeded || int64(output.Len()) > maxOutputBytes {
		return nil, ErrPreviewLimitExceeded
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateExactPNGStream(ctx, output.Bytes()); err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return nil, contextErr
		}
		return nil, ErrInvalidPreviewContent
	}
	return append([]byte(nil), output.Bytes()...), nil
}

func validateExactPNGStream(ctx context.Context, content []byte) error {
	const pngSignatureLength = 8
	if ctx == nil {
		return ErrInvalidPreviewContent
	}
	if len(content) < pngSignatureLength || !bytes.Equal(content[:pngSignatureLength], []byte("\x89PNG\r\n\x1a\n")) {
		return ErrInvalidPreviewContent
	}
	for offset := pngSignatureLength; ; {
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(content)-offset < 12 {
			return ErrInvalidPreviewContent
		}
		chunkLength := uint64(binary.BigEndian.Uint32(content[offset : offset+4]))
		chunkEnd := uint64(offset) + 12 + chunkLength
		if chunkEnd > uint64(len(content)) {
			return ErrInvalidPreviewContent
		}
		chunkType := content[offset+4 : offset+8]
		if bytes.Equal(chunkType, []byte("IEND")) {
			if chunkLength != 0 || chunkEnd != uint64(len(content)) {
				return ErrInvalidPreviewContent
			}
			return nil
		}
		offset = int(chunkEnd)
	}
}

func runPreviewCommand(
	ctx context.Context,
	binary string,
	arguments []string,
	stdout io.Writer,
	stderr io.Writer,
) error {
	workspace := ""
	for _, argument := range arguments {
		if filepath.Base(argument) == processorWorkspaceSourceName {
			workspace = filepath.Dir(argument)
			break
		}
	}
	if workspace == "" {
		return ErrInvalidPreviewContent
	}
	commandWorkspace, commandDirWorkspace, commandArguments, extraFiles, closeFiles, err := preparePreviewCommandDescriptor(workspace, arguments)
	if err != nil {
		return err
	}
	defer closeFiles()
	command := exec.CommandContext(ctx, binary, commandArguments...)
	command.ExtraFiles = extraFiles
	command.Env = previewCommandEnvironment(commandWorkspace)
	command.Dir = previewCommandWorkingDirectory(commandDirWorkspace)
	configurePreviewCommand(command)
	command.Stdin = nil
	command.Stdout = stdout
	command.Stderr = stderr
	return runPreviewCommandProcess(ctx, command)
}

func previewCommandEnvironment(workspace string) []string {
	return []string{
		"FONTCONFIG_FILE=" + filepath.Join(workspace, processorWorkspaceFontConfigName),
		"FONTCONFIG_PATH=" + filepath.Join(workspace, processorWorkspaceConfigName),
		"HOME=" + workspace,
		"LANG=C",
		"LC_ALL=C",
		"XDG_CACHE_HOME=" + filepath.Join(workspace, processorWorkspaceCacheName),
		"XDG_CONFIG_HOME=" + filepath.Join(workspace, processorWorkspaceConfigName),
		"TMPDIR=" + filepath.Join(workspace, processorWorkspaceTempName),
	}
}

func previewCommandWorkingDirectory(workspace string) string {
	return filepath.Join(workspace, previewCommandRootDirectoryName, previewCommandWorkingDirectoryName)
}

func preparePrivatePreviewCommandDirectories(paths processorWorkspacePaths) error {
	for _, path := range []string{
		filepath.Join(paths.workspace, processorWorkspaceCacheName),
		filepath.Join(paths.workspace, processorWorkspaceConfigName),
		filepath.Join(paths.workspace, processorWorkspaceTempName),
		filepath.Join(paths.workspace, previewCommandRootDirectoryName),
		previewCommandWorkingDirectory(paths.workspace),
	} {
		if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return ErrInvalidPreviewContent
		}
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return ErrInvalidPreviewContent
		}
		if err := os.Chmod(path, 0o700); err != nil {
			return ErrInvalidPreviewContent
		}
	}
	fontConfigPath := filepath.Join(paths.workspace, processorWorkspaceFontConfigName)
	fontConfig, err := buildPrivateFontConfig(paths.workspace)
	if err != nil {
		return ErrInvalidPreviewContent
	}
	file, err := os.OpenFile(fontConfigPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return ErrInvalidPreviewContent
	}
	writeErr := writeAndSyncPrivateFile(context.Background(), file, fontConfig)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		if removeErr := os.Remove(fontConfigPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return ErrInvalidPreviewContent
		}
		return ErrInvalidPreviewContent
	}
	info, err := os.Lstat(fontConfigPath)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return ErrInvalidPreviewContent
	}
	return nil
}

func buildPrivateFontConfig(workspace string) ([]byte, error) {
	var fontConfig bytes.Buffer
	fontConfig.WriteString("<?xml version=\"1.0\"?><!DOCTYPE fontconfig SYSTEM \"urn:fontconfig:fonts.dtd\"><fontconfig>")
	for _, directory := range []string{"/usr/local/share/fonts", "/usr/share/fonts"} {
		fontConfig.WriteString("<dir>")
		if err := xml.EscapeText(&fontConfig, []byte(directory)); err != nil {
			return nil, err
		}
		fontConfig.WriteString("</dir>")
	}
	fontConfig.WriteString("<cachedir>")
	if err := xml.EscapeText(&fontConfig, []byte(filepath.Join(workspace, processorWorkspaceCacheName))); err != nil {
		return nil, err
	}
	fontConfig.WriteString("</cachedir><config><rescan><int>0</int></rescan></config></fontconfig>")
	return fontConfig.Bytes(), nil
}

func writeAndSyncPrivateFile(ctx context.Context, file *os.File, content []byte) error {
	if ctx == nil || file == nil {
		return ErrInvalidPreviewContent
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	written, err := file.Write(content)
	if err == nil && written != len(content) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = file.Sync()
	}
	if err != nil {
		return err
	}
	return ctx.Err()
}

func readRegularFileBounded(ctx context.Context, path string, limit int64) ([]byte, error) {
	if limit <= 0 {
		return nil, ErrInvalidPreviewConfig
	}
	if err := validateRegularFile(path); err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, ErrInvalidPreviewContent
	}
	defer file.Close()
	return readRegularFileBoundedReader(ctx, file, limit)
}

func readRegularFileBoundedReader(ctx context.Context, reader io.Reader, limit int64) ([]byte, error) {
	if ctx == nil || reader == nil {
		return nil, ErrInvalidProcessorCommand
	}
	content, err := io.ReadAll(io.LimitReader(&contextReader{ctx: ctx, reader: reader}, limit+1))
	if err != nil {
		if contextError := ctx.Err(); contextError != nil {
			return nil, contextError
		}
		return nil, ErrInvalidPreviewContent
	}
	if int64(len(content)) > limit {
		return nil, ErrPreviewLimitExceeded
	}
	return content, nil
}

func validateRegularFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return ErrInvalidPreviewContent
	}
	return nil
}

func validateRegularFileSize(path string, limit int64) error {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return ErrInvalidPreviewContent
	}
	if info.Size() > limit {
		return ErrPreviewLimitExceeded
	}
	return nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader *contextReader) Read(buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(buffer)
}

type boundedPreviewBuffer struct {
	bytes.Buffer
	limit    int64
	exceeded bool
}

func (buffer *boundedPreviewBuffer) Write(content []byte) (int, error) {
	remaining := buffer.limit - int64(buffer.Len())
	if remaining <= 0 {
		buffer.exceeded = true
		return len(content), nil
	}
	toWrite := content
	if int64(len(toWrite)) > remaining {
		toWrite = toWrite[:remaining]
		buffer.exceeded = true
	}
	if _, err := buffer.Buffer.Write(toWrite); err != nil {
		return 0, err
	}
	return len(content), nil
}

func (artifact PreviewArtifact) String() string {
	return fmt.Sprintf("preview{present=%t media_type=%q size=%d}", artifact.HasPreview, artifact.MediaType, len(artifact.Bytes))
}
