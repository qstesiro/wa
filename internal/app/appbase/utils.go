// 版权 @2019 凹语言 作者。保留所有权利。

package appbase

import (
	"os"
	"strings"
	"unicode"
)

// 路径名是否包含后缀, 忽略大小写区别
func HasExt(path string, exts ...string) bool {
	if path == "" {
		return false
	}
	if len(exts) == 0 {
		return true
	}
	for _, ext := range exts {
		if len(path) <= len(ext) {
			continue
		}
		if strings.EqualFold(ext, path[len(path)-len(ext):]) {
			return true
		}
	}
	return false
}

// 本地路径存在
func PathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// 是否为本地存在的文件, 并满足后缀名
func IsNativeFile(path string, exts ...string) bool {
	if !HasExt(path, exts...) {
		return false
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return !fi.IsDir()
}

// 是否为本地存在的目录
func IsNativeDir(path string) bool {
	fi, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return fi.IsDir()
}

// 替换后缀名
func ReplaceExt(path string, extOld, extNew string) string {
	return path[:len(path)-len(extOld)] + extNew
}

// 为合法的 app 名字
func IsValidAppName(s string) bool {
	if s == "" || s[0] == '_' || (s[0] >= '0' && s[0] <= '9') {
		return false
	}
	for _, c := range []rune(s) {
		if c == '_' || (c >= '0' && c <= '9') || unicode.IsLetter(c) {
			continue
		}
		return false
	}
	return true
}

// 为合法的包路径
func IsValidPkgpath(s string) bool {
	if s == "" || s[0] == '_' || (s[0] >= '0' && s[0] <= '9') {
		return false
	}
	for _, c := range []rune(s) {
		if c == '_' || c == '.' || c == '/' || (c >= '0' && c <= '9') {
			continue
		}
		if unicode.IsLetter(c) {
			continue
		}
		return false
	}

	var pkgname = s
	if i := strings.LastIndex(s, "/"); i >= 0 {
		pkgname = s[i+1:]
	}
	return IsValidAppName(pkgname)
}

// 是标准库路径
