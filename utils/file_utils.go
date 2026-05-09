package utils

import (
	"bufio"
	"os"
	"path/filepath"
	"log"
	"sync"
)

// 清空文件内容
func ClearFileContent(filename string) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()
	return nil
}

// 追加内容到文件
func AppendToFile(filename, text string) error {
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.WriteString(text)
	return err
}

// BufferedWriter 持有文件句柄的缓冲写入器，避免每次写入都打开/关闭文件
type BufferedWriter struct {
	mu   sync.Mutex
	file *os.File
	buf  *bufio.Writer
}

// NewBufferedWriter 创建一个缓冲写入器，打开文件用于追加写入
func NewBufferedWriter(filename string) (*BufferedWriter, error) {
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return nil, err
	}
	return &BufferedWriter{
		file: f,
		buf:  bufio.NewWriter(f),
	}, nil
}

// Write 写入一行内容并刷新缓冲区
func (w *BufferedWriter) Write(text string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, err := w.buf.WriteString(text)
	if err != nil {
		return err
	}
	return w.buf.Flush()
}

// Close 关闭文件句柄
func (w *BufferedWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf.Flush()
	return w.file.Close()
}

// 删除流媒体文件
func DeleteStreamFiles() error {
	files, err := filepath.Glob("stream9527_*")
	if err != nil {
		return err
	}

	for _, file := range files {
		err := os.Remove(file)
		if err != nil {
			log.Printf("删除文件 %s 失败: %v\n", file, err)
		} else {
			log.Printf("成功删除文件 %s\n", file)
		}
	}

	return nil
}