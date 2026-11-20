/**
# Copyright (c) NVIDIA CORPORATION.  All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
**/

package coexistence

import (
	"testing"

	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestAdmissionRetentionAndIdempotence(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, admissionv1.AddToScheme(scheme))
	// Preserve Helm metadata when the operator reconciles the same policy for OLM.
	policy := &admissionv1.ValidatingAdmissionPolicy{ObjectMeta: metav1.ObjectMeta{Name: admissionPolicyName, Annotations: map[string]string{"helm.sh/resource-policy": "keep"}}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(policy).Build()
	require.NoError(t, EnsureAdmission(t.Context(), c))
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(policy), policy))
	require.Equal(t, "keep", policy.Annotations["helm.sh/resource-policy"])
	require.Empty(t, policy.OwnerReferences, "deleting a source must not garbage-collect admission")
	require.Equal(t, "GPUCluster", policy.Spec.ParamKind.Kind)
	require.Equal(t, []admissionv1.OperationType{admissionv1.Create}, policy.Spec.MatchConstraints.ResourceRules[0].Operations)
	require.Equal(t, []string{"clusterpolicies"}, policy.Spec.MatchConstraints.ResourceRules[0].Resources)
	require.Equal(t, "false", policy.Spec.Validations[0].Expression)
	revision := policy.ResourceVersion
	require.NoError(t, EnsureAdmission(t.Context(), c))
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(policy), policy))
	require.Equal(t, revision, policy.ResourceVersion)
	binding := &admissionv1.ValidatingAdmissionPolicyBinding{}
	require.NoError(t, c.Get(t.Context(), client.ObjectKey{Name: admissionPolicyName}, binding))
	require.NotNil(t, binding.Spec.ParamRef.Selector)
	require.Equal(t, admissionv1.AllowAction, *binding.Spec.ParamRef.ParameterNotFoundAction)
	require.Equal(t, []admissionv1.ValidationAction{admissionv1.Deny}, binding.Spec.ValidationActions)
}
