package utils

import (
	"os"
	"path/filepath"
	"log"
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