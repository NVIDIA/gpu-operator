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
	"context"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const admissionPolicyName = "gpu-operator-clusterpolicy-creation"

// EnsureAdmission also covers installations without Helm (including OLM).
// Retain this parameterized policy after source deletion: absence of GPUCluster
// allows CP creation. Ignore evaluation errors so a retained policy cannot block
// CP creation if the GPUCluster CRD is removed. The CP controller enforces ordering
// during reconciliation while admission registration or the parameter cache catches up.
func EnsureAdmission(ctx context.Context, c client.Client) error {
	policy := &admissionv1.ValidatingAdmissionPolicy{ObjectMeta: metav1.ObjectMeta{Name: admissionPolicyName}}
	if _, err := controllerutil.CreateOrUpdate(ctx, c, policy, func() error {
		policy.Spec = admissionv1.ValidatingAdmissionPolicySpec{
			FailurePolicy: new(admissionv1.Ignore),
			ParamKind:     &admissionv1.ParamKind{APIVersion: "nvidia.com/v1alpha1", Kind: "GPUCluster"},
			MatchConstraints: &admissionv1.MatchResources{MatchPolicy: new(admissionv1.Equivalent), NamespaceSelector: &metav1.LabelSelector{}, ObjectSelector: &metav1.LabelSelector{}, ResourceRules: []admissionv1.NamedRuleWithOperations{{RuleWithOperations: admissionv1.RuleWithOperations{
				Operations: []admissionv1.OperationType{admissionv1.Create},
				Rule:       admissionv1.Rule{APIGroups: []string{"nvidia.com"}, APIVersions: []string{"v1"}, Resources: []string{"clusterpolicies"}, Scope: new(admissionv1.ClusterScope)},
			}}}},
			Validations: []admissionv1.Validation{{Expression: "false", Message: "Cannot create ClusterPolicy while GPUCluster exists."}},
		}
		return nil
	}); err != nil {
		return err
	}
	binding := &admissionv1.ValidatingAdmissionPolicyBinding{ObjectMeta: metav1.ObjectMeta{Name: admissionPolicyName}}
	_, err := controllerutil.CreateOrUpdate(ctx, c, binding, func() error {
		binding.Spec = admissionv1.ValidatingAdmissionPolicyBindingSpec{PolicyName: admissionPolicyName, ValidationActions: []admissionv1.ValidationAction{admissionv1.Deny}, ParamRef: &admissionv1.ParamRef{Selector: &metav1.LabelSelector{}, ParameterNotFoundAction: new(admissionv1.AllowAction)}}
		return nil
	})
	return err
}
