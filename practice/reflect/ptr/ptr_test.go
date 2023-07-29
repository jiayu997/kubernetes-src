package ptr

import (
	"fmt"
	"reflect"
	"testing"
)

func TestPtrReflect(t *testing.T) {
	type cat struct{}

	// create cat instance
	ins := &cat{}

	typeOfCat := reflect.TypeOf(ins)

	fmt.Printf("name:'%v' kind:'%v'\n", typeOfCat.Name(), typeOfCat.Kind())

	// 取类型的元素即取出了cat{}
	// 取指针类型的元素类型，也就是 cat 类型。这个操作不可逆，不可以通过一个非指针类型获取它的指针类型。
	typeOfCat = typeOfCat.Elem()
	// 输出指针变量指向元素的类型名称和种类，得到了 cat 的类型名称（cat）和种类（struct）
	fmt.Printf("element name: '%v', element kind: '%v'\n", typeOfCat.Name(), typeOfCat.Kind())

	// 结果
	// name:'' kind:'ptr'
	// element name: 'cat', element kind: 'struct'
}

// 如果想通过反射修改变量 x，就要把想要修改的变量的指针传递给反射库
func TestModifyData(t *testing.T) {
	var x float64 = 3.4

	p := reflect.ValueOf(&x)
	//type of p: *float64
	fmt.Println("type of p:", p.Type())
	// settability of p: false
	fmt.Println("settability of p:", p.CanSet())

	// 反射对象 p 是不可写的，但是我们也不像修改 p，事实上我们要修改的是 *p。
	// 为了得到 p 指向的数据，可以调用 Value 类型的 Elem 方法。Elem 方法能够对指针进行“解引用”，
	// 然后将结果存储到反射 Value 类型对象 v 中

	v := p.Elem()
	// settability of v: true
	fmt.Println("settability of v:", v.CanSet())

	v.SetFloat(7.1)
	// 7.1
	fmt.Println(v.Interface())
	// 7.1
	fmt.Println(x)
}

//下面是一个解析结构体变量 t 的例子，用结构体的地址创建反射变量，再修改它。
//然后我们对它的类型设置了 typeOfT，并用调用简单的方法迭代字段。
//需要注意的是，我们从结构体的类型中提取了字段的名字，但每个字段本身是正常的 reflect.Value 对象

func TestStructPtr(tr *testing.T) {
	// T 字段名之所以大写，是因为结构体中只有可导出的字段是“可设置”的
	type T struct {
		A int
		B string
	}
	t := T{23, "skidoo"}

	s := reflect.ValueOf(&t)
	// *ptr.T ptr
	fmt.Println(s.Type(), s.Kind())

	// 取出&t 指针指向的对象
	svalue := s.Elem()
	// {23 skidoo}
	fmt.Println(svalue)

	// ptr.T
	fmt.Println(svalue.Type())
	typeOfT := svalue.Type()
	for i := 0; i < svalue.NumField(); i++ {
		f := svalue.Field(i)
		// 0: A int = 23
		// 1: B string = skidoo
		fmt.Printf("%d: %s %s = %v\n", i, typeOfT.Field(i).Name, f.Type(), f.Interface())
	}

	// 修改数据
	svalue.Field(0).SetInt(77)
	svalue.Field(1).SetString("Sunset Strip")
	// 如果我们修改了程序让 s 由 t（而不是 &t）创建，
	// 程序就会在调用 SetInt 和 SetString 的地方失败，因为 t 的字段是不可设置的
	// t is now {77 Sunset Strip}
	fmt.Println("t is now", t)
}
