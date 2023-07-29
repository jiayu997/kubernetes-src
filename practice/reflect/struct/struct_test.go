package _struct

import (
	"fmt"
	"reflect"
	"testing"
)

// StructTag 拥有一些方法，可以进行 Tag 信息的解析和提取，如下所示：
//
//	func (tag StructTag) Get(key string) string：根据 Tag 中的键获取对应的值，例如`key1:"value1" key2:"value2"`的 Tag 中，可以传入“key1”获得“value1”。
//	func (tag StructTag) Lookup(key string) (value string, ok bool)：根据 Tag 中的键，查询值是否存在。
func TestGetTag(t *testing.T) {
	type cat struct {
		Name string
		Type int `json:"type" id:"100"`
	}
	typeOfCat := reflect.TypeOf(cat{})
	if catType, ok := typeOfCat.FieldByName("Type"); ok {
		// get type
		fmt.Println(catType.Tag.Get("json"))
	}
}

func TestGetMember(t *testing.T) {
	type cat struct {
		Name string
		Type int `json:"type" id:"100"`
	}

	ins := cat{Name: "mini", Type: 1}

	// 获取结构体实例的反射类型对象
	typeOfCat := reflect.TypeOf(ins)

	// 遍历结构体所有成员
	for i := 0; i < typeOfCat.NumField(); i++ {
		// 获取每个成员的结构体字段类型
		fieldType := typeOfCat.Field(i)
		// 输出成员名和tag
		fmt.Printf("name: %v  tag: '%v'\n", fieldType.Name, fieldType.Tag)

	}
	// 通过字段名, 找到字段类型信息
	// 使用 reflect.Type 的 FieldByName() 根据字段名查找结构体字段信息，catType 表示返回的结构体字段信息，类型为 StructField，ok 表示是否找到结构体字段的信息
	if catType, ok := typeOfCat.FieldByName("Type"); ok {
		// 使用 StructField 中 Tag 的 Get() 方法，根据 Tag 中的名字进行信息获取
		fmt.Println(catType.Tag.Get("json"), catType.Tag.Get("id"))
	}

	// 结果
	//	name: Name  tag: ''
	// name: Type  tag: 'json:"type" id:"100"'
	// type 100
}
