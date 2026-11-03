package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

//go:embed payload.zip
var embeddedBundle []byte

var appVersion = "1.0.2"

const (
	maxBundleFiles = 50000
	maxBundleFile = int64(1 << 30)
	maxBundleTotal = uint64(2 << 30)
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--dtv-single-file-self-test" {
		if _, _, err := inspectBundle(); err != nil { fmt.Fprintln(os.Stderr, err); os.Exit(2) }
		return
	}
	if err := launch(); err != nil { showError(err.Error()); os.Exit(1) }
}

func showError(message string) {
	body, bodyErr := syscall.UTF16PtrFromString(message)
	title, titleErr := syscall.UTF16PtrFromString("D TV 啟動失敗")
	if bodyErr != nil || titleErr != nil { return }
	syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW").Call(0,
		uintptr(unsafe.Pointer(body)), uintptr(unsafe.Pointer(title)), 0x10|0x1000)
}

func safeBundlePath(name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	if name == "" || strings.ContainsRune(name, 0) || strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("封裝內含無效路徑：%q", name)
	}
	clean := path.Clean(name)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") ||
		path.IsAbs(clean) || strings.Contains(strings.Split(clean, "/")[0], ":") {
		return "", fmt.Errorf("封裝內含越界路徑：%q", name)
	}
	return filepath.FromSlash(clean), nil
}

func inspectBundle() (*zip.Reader, map[string]*zip.File, error) {
	if len(embeddedBundle) == 0 { return nil, nil, errors.New("找不到內嵌的 D TV 程式檔案") }
	reader, err := zip.NewReader(bytes.NewReader(embeddedBundle), int64(len(embeddedBundle)))
	if err != nil { return nil, nil, fmt.Errorf("單檔封裝 ZIP 無法讀取：%w", err) }
	if len(reader.File) == 0 || len(reader.File) > maxBundleFiles {
		return nil, nil, errors.New("單檔封裝內的檔案數量不合理")
	}
	files := make(map[string]*zip.File, len(reader.File))
	var total uint64
	for _, file := range reader.File {
		relative, pathErr := safeBundlePath(file.Name)
		if pathErr != nil { return nil, nil, pathErr }
		if file.Mode()&os.ModeSymlink != 0 { return nil, nil, fmt.Errorf("封裝不允許符號連結：%s", file.Name) }
		if file.UncompressedSize64 > uint64(maxBundleFile) { return nil, nil, fmt.Errorf("封裝檔案超過大小限制：%s", file.Name) }
		total += file.UncompressedSize64
		if total > maxBundleTotal { return nil, nil, errors.New("D TV 封裝總大小超過安全限制") }
		if !file.FileInfo().IsDir() && file.Mode().IsRegular() { files[filepath.ToSlash(relative)] = file }
	}
	for _, required := range []string{"DTV.exe", "duanju_core.dll", "flutter_windows.dll", "data/icudtl.dat", "data/app.so"} {
		if files[required] == nil { return nil, nil, fmt.Errorf("D TV 封裝缺少必要檔案：%s", required) }
	}
	for name := range files {
		if strings.HasPrefix(name, "data/flutter_assets/") { return reader, files, nil }
	}
	return nil, nil, errors.New("D TV 封裝缺少 Flutter 介面資源")
}

func launch() error {
	reader, _, err := inspectBundle()
	if err != nil { return err }
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" { return errors.New("Windows 尚未提供使用者 LOCALAPPDATA 路徑") }
	digest := sha256.Sum256(embeddedBundle)
	digestText := hex.EncodeToString(digest[:])
	installRoot := filepath.Join(localAppData, "DTV", "Portable")
	if err := os.MkdirAll(installRoot, 0755); err != nil { return fmt.Errorf("無法建立 D TV 使用者程式資料夾：%w", err) }
	installDir := filepath.Join(installRoot, appVersion+"-"+digestText[:12])
	marker := filepath.Join(installDir, ".dtv-bundle-sha256")
	if data, readErr := os.ReadFile(marker); readErr == nil && strings.TrimSpace(string(data)) == digestText {
		if info, statErr := os.Stat(filepath.Join(installDir, "DTV.exe")); statErr == nil && info.Mode().IsRegular() {
			return startApplication(installDir)
		}
	}
	staging, err := os.MkdirTemp(installRoot, ".dtv-extract-")
	if err != nil { return fmt.Errorf("無法建立 D TV 暫存資料夾：%w", err) }
	defer os.RemoveAll(staging)
	for _, file := range reader.File {
		relative, pathErr := safeBundlePath(file.Name)
		if pathErr != nil { return pathErr }
		target := filepath.Join(staging, relative)
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0755); err != nil { return err }
			continue
		}
		if !file.Mode().IsRegular() { continue }
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil { return err }
		source, err := file.Open()
		if err != nil { return fmt.Errorf("無法讀取封裝檔案 %s：%w", file.Name, err) }
		output, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
		if err != nil { source.Close(); return fmt.Errorf("無法寫入 D TV 檔案 %s：%w", file.Name, err) }
		written, copyErr := io.Copy(output, io.LimitReader(source, int64(file.UncompressedSize64)+1))
		sourceErr, outputErr := source.Close(), output.Close()
		if copyErr != nil || sourceErr != nil || outputErr != nil {
			return fmt.Errorf("解壓縮 D TV 檔案失敗 %s：%w", file.Name, errors.Join(copyErr, sourceErr, outputErr))
		}
		if uint64(written) != file.UncompressedSize64 { return fmt.Errorf("D TV 檔案大小驗證失敗：%s", file.Name) }
	}
	if err := os.WriteFile(filepath.Join(staging, ".dtv-bundle-sha256"), []byte(digestText+"\n"), 0644); err != nil {
		return fmt.Errorf("無法寫入 D TV 封裝驗證資料：%w", err)
	}
	if _, err := os.Stat(installDir); err == nil {
		if err := os.RemoveAll(installDir); err != nil { return fmt.Errorf("無法更新 D TV 程式檔案：%w", err) }
	}
	if err := os.Rename(staging, installDir); err != nil { return fmt.Errorf("無法完成 D TV 程式檔案部署：%w", err) }
	return startApplication(installDir)
}

func startApplication(installDir string) error {
	command := exec.Command(filepath.Join(installDir, "DTV.exe"), os.Args[1:]...)
	command.Dir = installDir
	if err := command.Start(); err != nil { return fmt.Errorf("D TV 主程式啟動失敗：%w", err) }
	return command.Process.Release()
}
