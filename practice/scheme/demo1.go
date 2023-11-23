package scheme

import (
	"encoding/json"
	"fmt"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"log"
	"reflect"
)

func printJson(obj interface{}) {
	data, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		log.Fatal("序列化失败!", err)
	}
	fmt.Println(string(data))
}

func Demo1() {
	deployment := &appsv1.Deployment{}
	s := scheme.Scheme

	// 通过类型找gvk
	// gvks = [apps/v1, Kind=Deployment]
	gvks, _, err := s.ObjectKinds(deployment)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(gvks)

	// 通过gvk创建对应的类型实例
	obj, err := s.New(gvks[0])
	if err != nil {
		log.Fatal(err)
	}

	t := reflect.TypeOf(obj)
	fmt.Println("type name: ", t.Elem().Name())
}
