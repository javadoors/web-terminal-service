/*
 * Copyright (c) 2024 Huawei Technologies Co., Ltd.
 * openFuyao is licensed under Mulan PSL v2.
 * You can use this software according to the terms and conditions of the Mulan PSL v2.
 * You may obtain a copy of Mulan PSL v2 at:
 *          http://license.coscl.org.cn/MulanPSL2
 * THIS SOFTWARE IS PROVIDED ON AN "AS IS" BASIS, WITHOUT WARRANTIES OF ANY KIND,
 * EITHER EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO NON-INFRINGEMENT,
 * MERCHANTABILITY OR FIT FOR A PARTICULAR PURPOSE.
 * See the Mulan PSL v2 for more details.
 */
package webterminal

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"reflect"
	"testing"

	"github.com/agiledragon/gomonkey/v2"
	"github.com/gorilla/websocket"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/controller-runtime/pkg/client"
	faker "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"openfuyao.com/web-terminal-service/api/v1beta1"
)

func CreateFakeClient() client.Client {
	Scheme := runtime.NewScheme()
	v1beta1.AddToScheme(Scheme)
	v1.AddToScheme(Scheme)
	fakeMgrClient := faker.NewClientBuilder().WithScheme(Scheme).Build()
	return fakeMgrClient
}

func TestNewTerminal(t *testing.T) {
	type args struct {
		client    kubernetes.Interface
		config    *rest.Config
		mgrclient client.Client
	}
	fakeMgrClient := CreateFakeClient()
	config := &rest.Config{Host: "hw"}
	client := fake.NewSimpleClientset()
	tests := []struct {
		name string
		args args
		want HandleInterface
	}{
		{
			name: "success",
			args: args{
				client:    client,
				config:    config,
				mgrclient: fakeMgrClient,
			},
			want: &terminaler{
				client:    client,
				config:    config,
				MgrClient: fakeMgrClient,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewTerminal(tt.args.client, tt.args.config, tt.args.mgrclient); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewTerminal() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsPodReady(t *testing.T) {
	type args struct {
		pod *v1.Pod
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "success",
			args: args{
				pod: &v1.Pod{
					Status: v1.PodStatus{
						Conditions: []v1.PodCondition{
							{
								Type:   v1.PodReady,
								Status: v1.ConditionTrue,
							},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "false",
			args: args{
				pod: &v1.Pod{
					Status: v1.PodStatus{
						Conditions: []v1.PodCondition{
							{
								Type:   v1.PodInitialized,
								Status: v1.ConditionTrue,
							},
						},
					},
				},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPodReady(tt.args.pod); got != tt.want {
				t.Errorf("isPodReady() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTerminalerCreateUserPod(t1 *testing.T) {
	type fields struct {
		client    kubernetes.Interface
		config    *rest.Config
		MgrClient client.Client
	}
	type args struct {
		ctx  context.Context
		user string
	}
	tests := []struct {
		name     string
		fields   fields
		args     args
		mockErr  error
		mockData string
	}{
		{
			name: "success",
			fields: fields{
				client:    fake.NewSimpleClientset(),
				config:    &rest.Config{},
				MgrClient: CreateFakeClient(),
			},
			args: args{
				ctx:  context.TODO(),
				user: "user",
			},
			mockData: "./",
			mockErr:  nil,
		},
	}
	for _, tt := range tests {
		t1.Run(tt.name, func(t1 *testing.T) {
			patch := gomonkey.ApplyFunc(getImagePath, func(path string) (string, error) {
				if tt.mockErr != nil {
					return "", tt.mockErr
				}
				return (tt.mockData), nil
			})
			defer patch.Reset()
			t := &terminaler{
				client:    tt.fields.client,
				config:    tt.fields.config,
				MgrClient: tt.fields.MgrClient,
			}
			t.CreateUserPod(tt.args.ctx, tt.args.user)
		})
	}
}

func TestGetImagePath(t *testing.T) {
	type args struct {
		filePath string
	}
	tests := []struct {
		name    string
		args    args
		want    string
		wantErr bool
	}{
		{
			name: "succeed",
			args: args{
				filePath: "",
			},
			want:    "Hello, this is a test file!",
			wantErr: false,
		},
		{
			name: "fail",
			args: args{
				filePath: "",
			},
			want:    "",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Step 1: 创建临时文件
			tmpFile, err := ioutil.TempFile("", "testfile-*.txt")
			if err != nil {
				t.Fatalf("Failed to create temp file: %v", err)
			}
			defer os.Remove(tmpFile.Name()) // 在测试结束时删除临时文件

			// Step 2: 向临时文件写入内容
			content := "Hello, this is a test file!"
			_, err = tmpFile.WriteString(content)
			if err != nil {
				t.Fatalf("Failed to write to temp file: %v", err)
			}

			// Step 3: 确保文件已关闭
			err = tmpFile.Close()
			if err != nil {
				t.Fatalf("Failed to close temp file: %v", err)
			}
			var got string
			// Step 4: 调用 getImagePath 读取临时文件
			if tt.name == "succeed" {
				got, err = getImagePath(tmpFile.Name())
			} else {
				got, err = getImagePath(tt.args.filePath)
			}
			if (err != nil) != tt.wantErr {
				t.Errorf("getImagePath() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			// Step 5: 验证读取的内容
			if got != tt.want {
				t.Errorf("getImagePath() got = %v, want %v", got, tt.want)
			}
		})
	}
}

type mockExecutor struct{}

func (m *mockExecutor) StreamWithContext(ctx context.Context, options remotecommand.StreamOptions) error {
	// 模拟命令输出
	if options.Stdout != nil {
		options.Stdout.Write([]byte("mock output"))
	}
	return nil
}

func (m *mockExecutor) Stream(options remotecommand.StreamOptions) error {
	// 模拟命令输出
	if options.Stdout != nil {
		options.Stdout.Write([]byte("mock output"))
	}
	return nil
}

func TestTerminalerStartSessionWithPing(t1 *testing.T) {
	conn := setupWebSockerServer(t1)
	defer conn.Close()

	type fields struct {
		client    kubernetes.Interface
		config    *rest.Config
		MgrClient client.Client
	}
	type args struct {
		ctx           context.Context
		namespace     string
		podName       string
		containerName string
		conn          *websocket.Conn
		mockErr       error
	}
	tests := []struct {
		name   string
		fields fields
		args   args
	}{
		{
			name: "ok",
			fields: fields{
				client:    fake.NewSimpleClientset(),
				config:    &rest.Config{},
				MgrClient: CreateFakeClient(),
			},
			args: args{
				ctx:       context.TODO(),
				namespace: "default",
				conn:      conn,
			},
		},
		{
			name: "mock err",
			fields: fields{
				client:    fake.NewSimpleClientset(),
				config:    &rest.Config{},
				MgrClient: CreateFakeClient(),
			},
			args: args{
				ctx:       context.TODO(),
				namespace: "system",
				conn:      conn,
			},
		},
	}
	for _, tt := range tests {
		t1.Run(tt.name, func(t1 *testing.T) {
			t := &terminaler{
				client:    tt.fields.client,
				config:    tt.fields.config,
				MgrClient: tt.fields.MgrClient,
			}
			patch := gomonkey.ApplyMethod(reflect.TypeOf(&terminaler{}), "HandleTerminal",
				func(_ *terminaler, ctx context.Context,
					namespace string, podName string, containerName string, conn *websocket.Conn) {
					fmt.Println("success")
				})
			defer patch.Reset()
			t.startSessionWithPing(tt.args.ctx, tt.args.namespace, tt.args.podName, tt.args.containerName, tt.args.conn)
		})
	}
}
