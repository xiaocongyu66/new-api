package model

import (
	"fmt"
	"reflect"
)

// typeNameOf 返回实体指针/值的类型名，供并发迁移错误信息使用。空指针或
// 非结构体类型回退到 fmt 占位，避免日志里出现 "<nil>"。
func typeNameOf(v any) string {
	if v == nil {
		return "<nil>"
	}
	t := reflect.TypeOf(v)
	if t == nil {
		return "<nil>"
	}
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return fmt.Sprintf("%T", v)
	}
	return t.Name()
}
