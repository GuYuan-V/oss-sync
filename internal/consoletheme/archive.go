package consoletheme

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

const (
	maxArchiveBytes = 32 << 20
	maxEntryBytes   = 8 << 20
)

func Upload(dataDir, name string, reader io.ReaderAt, size int64) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	if IsBuiltin(name) {
		return ErrReadOnly
	}
	if size <= 0 || size > maxArchiveBytes {
		return errors.New("服务器主题 ZIP 大小超出限制")
	}
	archive, err := zip.NewReader(reader, size)
	if err != nil {
		return fmt.Errorf("无效的服务器主题 ZIP: %w", err)
	}
	if len(archive.File) == 0 || len(archive.File) > maxEntries {
		return errors.New("服务器主题 ZIP 文件数不合法")
	}
	entries := make([]archiveEntry, 0, len(archive.File))
	var total int64
	hasCSS := false
	for _, file := range archive.File {
		if file.FileInfo().IsDir() {
			continue
		}
		if file.Mode()&os.ModeSymlink != 0 || !safeEntry(file.Name) {
			return fmt.Errorf("服务器主题 ZIP 包含非法路径: %s", file.Name)
		}
		if file.UncompressedSize64 > maxEntryBytes {
			return fmt.Errorf("服务器主题文件超出上限: %s", file.Name)
		}
		total += int64(file.UncompressedSize64)
		if total > maxArchiveBytes {
			return errors.New("服务器主题解压后总大小超出限制")
		}
		rc, err := file.Open()
		if err != nil {
			return err
		}
		content, readErr := io.ReadAll(io.LimitReader(rc, maxEntryBytes+1))
		_ = rc.Close()
		if readErr != nil || len(content) > maxEntryBytes {
			return errors.New("读取服务器主题 ZIP 失败")
		}
		entries = append(entries, archiveEntry{name: filepath.ToSlash(file.Name), content: content})
		if filepath.ToSlash(file.Name) == "theme.css" {
			hasCSS = true
		}
	}
	if !hasCSS {
		return errors.New("服务器主题 ZIP 必须包含 theme.css")
	}
	target, err := createThemeDir(dataDir, name)
	if err != nil {
		return err
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.RemoveAll(target)
		}
	}()
	for _, entry := range entries {
		if err := writeFile(target, entry.name, entry.content); err != nil {
			return err
		}
	}
	complete = true
	return nil
}

func CreateZip(dataDir, name string, writer *zip.Writer) error {
	if IsBuiltin(name) {
		return ErrReadOnly
	}
	dir, err := themeDir(dataDir, name)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	return filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("服务器主题包含符号链接")
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(rel)
		header.Method = zip.Deflate
		destination, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}
		source, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(destination, source)
		closeErr := source.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}

type archiveEntry struct {
	name    string
	content []byte
}
