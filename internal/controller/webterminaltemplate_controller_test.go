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

package controller

import (
	"context"
	stdErrors "errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"openfuyao.com/web-terminal-service/api/v1beta1"
)

type reconcileTestCase struct {
	name         string
	existingObjs []client.Object
	req          ctrl.Request
	expectErr    bool
	verify       func(t *testing.T, c client.Client, result ctrl.Result)
}

type updateStatusTestCase struct {
	name           string
	existingObj    *v1beta1.WebterminalTemplate
	targetObj      *v1beta1.WebterminalTemplate
	newStatus      v1beta1.WebterminalTemplateStatus
	mockUpdateFail bool
	expectPhase    v1beta1.WebTerminalTemplatePhase
	expectRequeue  time.Duration
}

func setupScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(s))
	utilruntime.Must(v1beta1.AddToScheme(s))
	return s
}

func newReadyPod(name, namespace string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}
}

func TestWebterminalTemplateReconcilerReconcile(t *testing.T) {
	scheme := setupScheme()
	testCases := getReconcileTestCases()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			executeReconcileTest(t, tc, scheme)
		})
	}
}

const (
	bufferSize = 10
	updateTime = -30 * time.Minute
)

func executeReconcileTest(t *testing.T, tc reconcileTestCase, scheme *runtime.Scheme) {
	clientBuilder := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(tc.existingObjs...).
		WithStatusSubresource(tc.existingObjs...)

	fakeClient := clientBuilder.Build()
	recorder := record.NewFakeRecorder(bufferSize)

	r := &WebterminalTemplateReconciler{
		Client:   fakeClient,
		Scheme:   scheme,
		Recorder: recorder,
	}

	result, err := r.Reconcile(context.Background(), tc.req)

	if tc.expectErr {
		assert.Error(t, err)
	} else {
		assert.NoError(t, err)
	}

	if tc.verify != nil {
		tc.verify(t, fakeClient, result)
	}
}

func getReconcileTestCases() []reconcileTestCase {
	now := metav1.Now()
	return []reconcileTestCase{
		{
			name:         "Scenario 1: Resource Not Found",
			existingObjs: []client.Object{},
			req: ctrl.Request{
				NamespacedName: types.NamespacedName{Name: "test-term", Namespace: "default"},
			},
			expectErr: false,
			verify: func(t *testing.T, c client.Client, result ctrl.Result) {
				assert.Equal(t, ctrl.Result{}, result)
			},
		},
		{
			name: "Scenario 2: Add Finalizer",
			existingObjs: []client.Object{
				&v1beta1.WebterminalTemplate{
					ObjectMeta: metav1.ObjectMeta{Name: "test-term", Namespace: "default"},
					Spec: v1beta1.WebterminalTemplateSpec{
						PodTemplate: v1beta1.PodTemplate{
							ObjectMeta: v1beta1.PodTemplateObjectMeta{Name: "test-term", Namespace: "default"},
							Spec:       v1beta1.PodTemplateSpec{Containers: []corev1.Container{{Name: "c", Image: "i"}}},
						},
					},
				},
				newReadyPod("test-term", "default"),
			},
			req:       ctrl.Request{NamespacedName: types.NamespacedName{Name: "test-term", Namespace: "default"}},
			expectErr: false,
			verify: func(t *testing.T, c client.Client, result ctrl.Result) {
				fetched := &v1beta1.WebterminalTemplate{}
				_ = c.Get(context.TODO(), types.NamespacedName{Name: "test-term", Namespace: "default"}, fetched)
				assert.Contains(t, fetched.Finalizers, finalizer)
			},
		},
		{
			name: "Scenario 3: TTL Expiration",
			existingObjs: []client.Object{
				&v1beta1.WebterminalTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "expired-term",
						Namespace:  "default",
						Finalizers: []string{finalizer},
					},
					Spec: v1beta1.WebterminalTemplateSpec{
						RenewTime:  metav1.NewTime(time.Now().Add(updateTime)),
						ExistsTime: metav1.NewTime(time.Now().Add(updateTime)),
					},
					Status: v1beta1.WebterminalTemplateStatus{Phase: v1beta1.WebTerminalTemplateRunning},
				},
			},
			req:       ctrl.Request{NamespacedName: types.NamespacedName{Name: "expired-term", Namespace: "default"}},
			expectErr: false,
			verify: func(t *testing.T, c client.Client, result ctrl.Result) {
				fetched := &v1beta1.WebterminalTemplate{}
				err := c.Get(context.TODO(), types.NamespacedName{Name: "expired-term", Namespace: "default"}, fetched)
				assert.NoError(t, err)
				assert.False(t, fetched.DeletionTimestamp.IsZero(), "Object should be marked for deletion")
			},
		},
		{
			name: "Scenario 4: Logic after Pod Creation",
			existingObjs: []client.Object{
				&v1beta1.WebterminalTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "new-term",
						Namespace:  "default",
						Finalizers: []string{finalizer},
					},
					Spec: v1beta1.WebterminalTemplateSpec{
						PodTemplate: v1beta1.PodTemplate{
							ObjectMeta: v1beta1.PodTemplateObjectMeta{Name: "new-term", Namespace: "default"},
							Spec: v1beta1.PodTemplateSpec{
								Containers: []corev1.Container{{Name: "shell", Image: "busybox"}},
							},
						},
					},
				},
				newReadyPod("new-term", "default"),
			},
			req:       ctrl.Request{NamespacedName: types.NamespacedName{Name: "new-term", Namespace: "default"}},
			expectErr: false,
			verify: func(t *testing.T, c client.Client, result ctrl.Result) {
				fetched := &v1beta1.WebterminalTemplate{}
				err := c.Get(context.TODO(), types.NamespacedName{Name: "new-term", Namespace: "default"}, fetched)
				assert.NoError(t, err)
				assert.False(t, fetched.Spec.ExistsTime.IsZero())
				assert.Equal(t, v1beta1.WebTerminalTemplateRunning, fetched.Status.Phase)
			},
		},
		{
			name: "Scenario 5: Finalizer Cleanup",
			existingObjs: []client.Object{
				&v1beta1.WebterminalTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "deleting-term",
						Namespace:         "default",
						Finalizers:        []string{finalizer},
						DeletionTimestamp: &now,
					},
				},
				newReadyPod("deleting-term", "default"),
			},
			req:       ctrl.Request{NamespacedName: types.NamespacedName{Name: "deleting-term", Namespace: "default"}},
			expectErr: false,
			verify: func(t *testing.T, c client.Client, result ctrl.Result) {
				pod := &corev1.Pod{}
				err := c.Get(context.TODO(), types.NamespacedName{Name: "deleting-term", Namespace: "default"}, pod)
				assert.True(t, errors.IsNotFound(err), "Pod should be deleted")

				fetched := &v1beta1.WebterminalTemplate{}
				err = c.Get(context.TODO(), types.NamespacedName{Name: "deleting-term", Namespace: "default"}, fetched)
				assert.True(t, errors.IsNotFound(err), "Template should be deleted")
			},
		},
	}
}

func TestWebterminalTemplateReconcilerUpdateStatus(t *testing.T) {
	scheme := setupScheme()
	testCases := getUpdateStatusTestCases()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			executeUpdateStatusTest(t, tc, scheme)
		})
	}
}

func executeUpdateStatusTest(t *testing.T, tc updateStatusTestCase, scheme *runtime.Scheme) {
	builder := fake.NewClientBuilder().WithScheme(scheme)
	if tc.existingObj != nil {
		builder.WithObjects(tc.existingObj).WithStatusSubresource(tc.existingObj)
	}

	if tc.mockUpdateFail {
		builder.WithInterceptorFuncs(interceptor.Funcs{
			SubResourceUpdate: func(ctx context.Context, client client.Client, subResourceName string,
				obj client.Object, opts ...client.SubResourceUpdateOption) error {
				return stdErrors.New("mock update error")
			},
		})
	}

	fakeClient := builder.Build()

	r := &WebterminalTemplateReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	result, err := r.updateStatus(context.Background(), tc.targetObj, tc.newStatus)

	assert.NoError(t, err)
	assert.Equal(t, tc.expectRequeue, result.RequeueAfter)

	if tc.existingObj != nil && !tc.mockUpdateFail {
		updatedObj := &v1beta1.WebterminalTemplate{}
		err := fakeClient.Get(context.Background(),
			types.NamespacedName{Name: tc.targetObj.Name, Namespace: tc.targetObj.Namespace}, updatedObj)
		assert.NoError(t, err)
		assert.Equal(t, tc.expectPhase, updatedObj.Status.Phase)

		if len(tc.newStatus.Conditions) > 0 {
			assert.Equal(t, tc.newStatus.Conditions[0].Reason, updatedObj.Status.Conditions[0].Reason)
		}
	}
}

func getUpdateStatusTestCases() []updateStatusTestCase {
	return []updateStatusTestCase{
		{
			name: "Success: Status should be updated",
			existingObj: &v1beta1.WebterminalTemplate{
				ObjectMeta: metav1.ObjectMeta{Name: "term-1", Namespace: "default"},
				Status:     v1beta1.WebterminalTemplateStatus{Phase: v1beta1.WebTerminalTemplateStarting},
			},
			targetObj: &v1beta1.WebterminalTemplate{
				ObjectMeta: metav1.ObjectMeta{Name: "term-1", Namespace: "default"},
			},
			newStatus: v1beta1.WebterminalTemplateStatus{
				Phase: v1beta1.WebTerminalTemplateRunning,
				Conditions: []v1beta1.WebTerminalTemplateCondition{
					{Reason: "Test", Status: "True"},
				},
			},
			expectPhase:   v1beta1.WebTerminalTemplateRunning,
			expectRequeue: defaultUpdateFrequency,
		},
		{
			name:        "Failure: Object not found",
			existingObj: nil,
			targetObj: &v1beta1.WebterminalTemplate{
				ObjectMeta: metav1.ObjectMeta{Name: "term-missing", Namespace: "default"},
			},
			newStatus:     v1beta1.WebterminalTemplateStatus{Phase: v1beta1.WebTerminalTemplateRunning},
			expectRequeue: 0,
		},
		{
			name: "Failure: Update fails",
			existingObj: &v1beta1.WebterminalTemplate{
				ObjectMeta: metav1.ObjectMeta{Name: "term-fail", Namespace: "default"},
			},
			targetObj: &v1beta1.WebterminalTemplate{
				ObjectMeta: metav1.ObjectMeta{Name: "term-fail", Namespace: "default"},
			},
			newStatus:      v1beta1.WebterminalTemplateStatus{Phase: v1beta1.WebTerminalTemplateRunning},
			mockUpdateFail: true,
			expectRequeue:  0,
		},
	}
}
