/*
Copyright 2022 NVIDIA CORPORATION & AFFILIATES

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package upgrade

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	"k8s.io/kubectl/pkg/drain"

	v1alpha1 "github.com/NVIDIA/k8s-operator-libs/api/upgrade/v1alpha1"
	"github.com/NVIDIA/k8s-operator-libs/pkg/consts"
)

// PodManagerImpl implements PodManager interface and checks for pod states
type PodManagerImpl struct {
	k8sInterface             kubernetes.Interface
	nodeUpgradeStateProvider NodeUpgradeStateProvider
	podDeletionFilter        PodDeletionFilter
	nodesInProgress          *StringSet
	log                      logr.Logger
	eventRecorder            record.EventRecorder
}

// PodManager is an interface that allows to wait on certain pod statuses
type PodManager interface {
	ScheduleCheckOnPodCompletion(ctx context.Context, config *PodManagerConfig) error
	SchedulePodsRestart(ctx context.Context, pods []*corev1.Pod) error
	SchedulePodEviction(ctx context.Context, config *PodManagerConfig) error
	GetPodDeletionFilter() PodDeletionFilter
	GetPodControllerRevisionHash(ctx context.Context, pod *corev1.Pod) (string, error)
	GetDaemonsetControllerRevisionHash(ctx context.Context, daemonset *appsv1.DaemonSet) (string, error)
}

// PodManagerConfig represent the selector for pods and Node names to be considered for managing those pods
type PodManagerConfig struct {
	Nodes                 []*corev1.Node
	DeletionSpec          *v1alpha1.PodDeletionSpec
	WaitForCompletionSpec *v1alpha1.WaitForCompletionSpec
	DrainEnabled          bool
}

const (
	PodControllerRevisionHashLabelKey = "controller-revision-hash"
)

// PodDeletionFilter takes a pod and returns a boolean indicating whether the pod should be deleted
type PodDeletionFilter func(corev1.Pod) bool

func (m *PodManagerImpl) GetPodDeletionFilter() PodDeletionFilter {
	return m.podDeletionFilter
}

func (m *PodManagerImpl) GetPodControllerRevisionHash(ctx context.Context, pod *corev1.Pod) (string, error) {
	if hash, ok := pod.Labels[PodControllerRevisionHashLabelKey]; ok {
		return hash, nil
	}
	return "", fmt.Errorf("controller-revision-hash label not present for pod %s", pod.Name)
}

func (m *PodManagerImpl) GetDaemonsetControllerRevisionHash(ctx context.Context, daemonset *appsv1.DaemonSet) (string, error) {
	hash := ""
	controllerRevisionList := &appsv1.ControllerRevisionList{}

	// get all revisions for the daemonset
	listOptions := meta_v1.ListOptions{LabelSelector: labels.SelectorFromSet(daemonset.Spec.Selector.MatchLabels).String()}
	controllerRevisionList, err := m.k8sInterface.AppsV1().ControllerRevisions(daemonset.Namespace).List(ctx, listOptions)
	if err != nil {
		return "", fmt.Errorf("error getting controller revision list for daemonset %s: %v", daemonset.Name, err)
	}

	var revisions = make([]int64, len(controllerRevisionList.Items))
	for i, controllerRevision := range controllerRevisionList.Items {
		revisions[i] = controllerRevision.Revision
	}

	// sort the revision list to make sure we obtain latest revision always
	sort.Slice(revisions, func(i, j int) bool { return revisions[i] < revisions[j] })

	currentRevision := revisions[len(revisions)-1]
	for _, controllerRevision := range controllerRevisionList.Items {
		if controllerRevision.Revision != currentRevision {
			continue
		}
		// trim the daemonset name: nvidia-driver-daemonset-5698dd78cf
		re := regexp.MustCompile(".*-")
		hash = re.ReplaceAllString(controllerRevision.Name, "")
		break
	}
	return hash, nil
}

// SchedulePodEviction receives a config for pod eviction and deletes pods for each node in the list.
// The set of pods to delete is determined by a filter that is provided to the PodManagerImpl during construction.
func (m *PodManagerImpl) SchedulePodEviction(ctx context.Context, config *PodManagerConfig) error {
	m.log.V(consts.LogLevelInfo).Info("Starting Pod Deletion")

	if len(config.Nodes) == 0 {
		m.log.V(consts.LogLevelInfo).Info("No nodes scheduled for pod deletion")
		return nil
	}

	podDeletionSpec := config.DeletionSpec

	if podDeletionSpec == nil {
		return fmt.Errorf("pod deletion spec should not be empty")
	}

	// Create a custom drain filter which will be passed to the drain helper.
	// The drain helper will carry out the actual deletion of pods on a node.
	customDrainFilter := func(pod corev1.Pod) drain.PodDeleteStatus {
		delete := m.podDeletionFilter(pod)
		if !delete {
			return drain.MakePodDeleteStatusSkip()
		}
		return drain.MakePodDeleteStatusOkay()
	}

	drainHelper := drain.Helper{
		Ctx:                 ctx,
		Client:              m.k8sInterface,
		Out:                 os.Stdout,
		ErrOut:              os.Stderr,
		GracePeriodSeconds:  -1,
		IgnoreAllDaemonSets: true,
		DeleteEmptyDirData:  podDeletionSpec.DeleteEmptyDir,
		Force:               podDeletionSpec.Force,
		Timeout:             time.Duration(podDeletionSpec.TimeoutSecond) * time.Second,
		AdditionalFilters:   []drain.PodFilter{customDrainFilter},
	}

	for _, node := range config.Nodes {
		if !m.nodesInProgress.Has(node.Name) {
			m.log.V(consts.LogLevelInfo).Info("Deleting pods on node", "node", node.Name)
			m.nodesInProgress.Add(node.Name)

			go func(node corev1.Node) {
				defer m.nodesInProgress.Remove(node.Name)

				m.log.V(consts.LogLevelInfo).Info("Identifying pods to delete", "node", node.Name)

				// List all pods
				podList, err := m.ListPods(ctx, "", node.Name)
				if err != nil {
					m.log.V(consts.LogLevelError).Error(err, "Failed to list pods", "node", node.Name)
					return
				}

				// Get number of pods requiring deletion using the podDeletionFilter
				numPodsToDelete := 0
				for _, pod := range podList.Items {
					if m.podDeletionFilter(pod) == true {
						numPodsToDelete += 1
					}
				}

				if numPodsToDelete == 0 {
					m.log.V(consts.LogLevelInfo).Info("No pods require deletion", "node", node.Name)
					_ = m.nodeUpgradeStateProvider.ChangeNodeUpgradeState(ctx, &node, UpgradeStatePodRestartRequired)
					return
				}

				m.log.V(consts.LogLevelInfo).Info("Identifying which pods can be deleted", "node", node.Name)
				podDeleteList, errs := drainHelper.GetPodsForDeletion(node.Name)

				numPodsCanDelete := len(podDeleteList.Pods())
				if numPodsCanDelete != numPodsToDelete {
					m.log.V(consts.LogLevelError).Error(nil, "Cannot delete all required pods", "node", node.Name)
					if errs != nil {
						for _, err := range errs {
							m.log.V(consts.LogLevelError).Error(err, "Error reported by drain helper", "node", node.Name)
						}
					}
					m.updateNodeToDrainOrFailed(ctx, node, config.DrainEnabled)
					return
				}

				for _, p := range podDeleteList.Pods() {
					m.log.V(consts.LogLevelInfo).Info("Identified pod to delete", "node", node.Name, "namespace", p.Namespace, "name", p.Name)
				}
				m.log.V(consts.LogLevelDebug).Info("Warnings when identifying pods to delete", "warnings", podDeleteList.Warnings(), "node", node.Name)

				err = drainHelper.DeleteOrEvictPods(podDeleteList.Pods())
				if err != nil {
					m.log.V(consts.LogLevelError).Error(err, "Failed to delete pods on the node", "node", node.Name)
					logEventf(m.eventRecorder, &node, corev1.EventTypeWarning, GetEventReason(), "Failed to delete workload pods on the node for the driver upgrade, %s", err.Error())
					m.updateNodeToDrainOrFailed(ctx, node, config.DrainEnabled)
					return
				}

				m.log.V(consts.LogLevelInfo).Info("Deleted pods on the node", "node", node.Name)
				_ = m.nodeUpgradeStateProvider.ChangeNodeUpgradeState(ctx, &node, UpgradeStatePodRestartRequired)
				logEvent(m.eventRecorder, &node, corev1.EventTypeNormal, GetEventReason(), "Deleted workload pods on the node for the driver upgrade")
			}(*node)
		} else {
			m.log.V(consts.LogLevelInfo).Info("Node is already getting pods deleted, skipping", "node", node.Name)
		}
	}
	return nil
}

// SchedulePodsRestart receives a list of pods and schedules to delete them
// TODO, schedule deletion of pods in parallel on all nodes
func (m *PodManagerImpl) SchedulePodsRestart(ctx context.Context, pods []*corev1.Pod) error {
	m.log.V(consts.LogLevelInfo).Info("Starting Pod Delete")
	if len(pods) == 0 {
		m.log.V(consts.LogLevelInfo).Info("No pods scheduled to restart")
		return nil
	}
	for _, pod := range pods {
		m.log.V(consts.LogLevelInfo).Info("Deleting pod", "pod", pod.Name)
		deleteOptions := meta_v1.DeleteOptions{}
		err := m.k8sInterface.CoreV1().Pods(pod.Namespace).Delete(ctx, pod.Name, deleteOptions)
		if err != nil {
			m.log.V(consts.LogLevelInfo).Error(err, "Failed to delete pod", "pod", pod.Name)
			logEventf(m.eventRecorder, pod, corev1.EventTypeWarning, GetEventReason(), "Failed to restart driver pod %s", err.Error())
			return err
		}
	}
	return nil
}

// ScheduleCheckOnPodCompletion receives PodSelectorConfig and schedules checks for pod statuses on each node in the list.
// If the checks are successful, the node moves to UpgradeStatePodDeletionRequired state,
// otherwise it will stay in the same current state.
func (m *PodManagerImpl) ScheduleCheckOnPodCompletion(ctx context.Context, config *PodManagerConfig) error {
	m.log.V(consts.LogLevelInfo).Info("Pod Manager, starting checks on pod statuses")
	var wg sync.WaitGroup

	for _, node := range config.Nodes {
		m.log.V(consts.LogLevelInfo).Info("Schedule checks for pod completion", "node", node.Name)
		// fetch the pods using the label selector provided
		podList, err := m.ListPods(ctx, config.WaitForCompletionSpec.PodSelector, node.Name)
		if err != nil {
			m.log.V(consts.LogLevelError).Error(err, "Failed to list pods", "selector", config.WaitForCompletionSpec.PodSelector, "node", node.Name)
			return err
		}
		if len(podList.Items) > 0 {
			m.log.V(consts.LogLevelDebug).Error(err, "Found workload pods", "selector", config.WaitForCompletionSpec.PodSelector, "node", node.Name, "pods", len(podList.Items))
		}
		// Increment the WaitGroup counter.
		wg.Add(1)
		go func(node corev1.Node) {
			// Decrement the counter when the goroutine completes.
			defer wg.Done()
			running := false
			for _, pod := range podList.Items {
				running = m.IsPodRunningOrPending(pod)
				if running {
					break
				}
			}
			// if workload pods are running, then check if timeout is specified and exceeded.
			// if no timeout is specified, then ignore the state updates and wait for completions.
			if running {
				m.log.V(consts.LogLevelInfo).Info("Workload pods are still running on the node", "node", node.Name)
				// check whether timeout is provided and is exceeded for job completions
				if config.WaitForCompletionSpec.TimeoutSecond != 0 {
					err = m.HandleTimeoutOnPodCompletions(ctx, &node, int64(config.WaitForCompletionSpec.TimeoutSecond))
					if err != nil {
						logEventf(m.eventRecorder, &node, corev1.EventTypeWarning, GetEventReason(), "Failed to handle timeout for job completions, %s", err.Error())
						return
					}
				}
				return
			}
			// remove annotation used for tracking start time
			annotationKey := GetWaitForPodCompletionStartTimeAnnotationKey()
			err = m.nodeUpgradeStateProvider.ChangeNodeUpgradeAnnotation(ctx, &node, annotationKey, "null")
			if err != nil {
				logEventf(m.eventRecorder, &node, corev1.EventTypeWarning, GetEventReason(), "Failed to remove annotation used to track job completions: %s", err.Error())
				return
			}
			// update node state
			_ = m.nodeUpgradeStateProvider.ChangeNodeUpgradeState(ctx, &node, UpgradeStatePodDeletionRequired)
			m.log.V(consts.LogLevelInfo).Info("Updated the node state", "node", node.Name, "state", UpgradeStatePodDeletionRequired)
		}(*node)
	}
	// Wait for all goroutines to complete
	wg.Wait()
	return nil
}

// ListPods returns the list of pods in all namespaces with the given selector
func (m *PodManagerImpl) ListPods(ctx context.Context, selector string, nodeName string) (*corev1.PodList, error) {
	listOptions := meta_v1.ListOptions{LabelSelector: selector, FieldSelector: "spec.nodeName=" + nodeName}
	podList, err := m.k8sInterface.CoreV1().Pods("").List(ctx, listOptions)
	if err != nil {
		return nil, err
	}
	return podList, nil
}

// HandleTimeoutOnPodCompletions transitions node based on the timeout for job completions on the node
func (m *PodManagerImpl) HandleTimeoutOnPodCompletions(ctx context.Context, node *corev1.Node, timeoutSeconds int64) error {
	annotationKey := GetWaitForPodCompletionStartTimeAnnotationKey()
	currentTime := time.Now().Unix()
	// check if annotation already exists for tracking start time
	if _, present := node.Annotations[annotationKey]; !present {
		// add the annotation to track start time
		err := m.nodeUpgradeStateProvider.ChangeNodeUpgradeAnnotation(ctx, node, annotationKey, strconv.FormatInt(currentTime, 10))
		if err != nil {
			m.log.V(consts.LogLevelError).Error(err, "Failed to add annotation to track job completions", "node", node.Name, "annotation", annotationKey)
			return err
		}
		return nil
	}
	// check if timeout reached
	startTime, err := strconv.ParseInt(node.Annotations[annotationKey], 10, 64)
	if err != nil {
		m.log.V(consts.LogLevelError).Error(err, "Failed to convert start time to track job completions", "node", node.Name)
		return err
	}
	if currentTime > startTime+timeoutSeconds {
		// timeout exceeded, mark node for pod/job deletions
		_ = m.nodeUpgradeStateProvider.ChangeNodeUpgradeState(ctx, node, UpgradeStatePodDeletionRequired)
		m.log.V(consts.LogLevelInfo).Info("Timeout exceeded for job completions, updated the node state", "node", node.Name, "state", UpgradeStatePodDeletionRequired)
		// remove annotation used for tracking start time
		err = m.nodeUpgradeStateProvider.ChangeNodeUpgradeAnnotation(ctx, node, annotationKey, "null")
		if err != nil {
			m.log.V(consts.LogLevelError).Error(err, "Failed to remove annotation used to track job completions", "node", node.Name, "annotation", annotationKey)
			return err
		}
	}
	return nil
}

// IsPodRunningOrPending returns true when the given pod is currently in Running or Pending state
func (m *PodManagerImpl) IsPodRunningOrPending(pod corev1.Pod) bool {
	switch pod.Status.Phase {
	case corev1.PodRunning:
		m.log.V(consts.LogLevelDebug).Info("Pod status", "pod", pod.Name, "node", pod.Spec.NodeName, "state", corev1.PodRunning)
		return true
	case corev1.PodPending:
		m.log.V(consts.LogLevelInfo).Info("Pod status", "pod", pod.Name, "node", pod.Spec.NodeName, "state", corev1.PodPending)
		return true
	case corev1.PodFailed:
		m.log.V(consts.LogLevelInfo).Info("Pod status", "pod", pod.Name, "node", pod.Spec.NodeName, "state", corev1.PodFailed)
		return false
	case corev1.PodSucceeded:
		m.log.V(consts.LogLevelInfo).Info("Pod status", "pod", pod.Name, "node", pod.Spec.NodeName, "state", corev1.PodSucceeded)
		return false
	}
	return false
}

func (m *PodManagerImpl) updateNodeToDrainOrFailed(ctx context.Context, node corev1.Node, drainEnabled bool) {
	nextState := UpgradeStateFailed
	if drainEnabled {
		m.log.V(consts.LogLevelInfo).Info("Pod deletion failed but drain is enabled in spec. Will attempt a node drain", "node", node.Name)
		logEvent(m.eventRecorder, &node, corev1.EventTypeWarning, GetEventReason(), "Pod deletion failed but drain is enabled in spec. Will attempt a node drain")
		nextState = UpgradeStateDrainRequired
	}
	_ = m.nodeUpgradeStateProvider.ChangeNodeUpgradeState(ctx, &node, nextState)
	return
}

// NewPodManager returns an instance of PodManager implementation
func NewPodManager(
	k8sInterface kubernetes.Interface,
	nodeUpgradeStateProvider NodeUpgradeStateProvider,
	log logr.Logger,
	podDeletionFilter PodDeletionFilter,
	eventRecorder record.EventRecorder) *PodManagerImpl {
	mgr := &PodManagerImpl{
		k8sInterface:             k8sInterface,
		log:                      log,
		nodeUpgradeStateProvider: nodeUpgradeStateProvider,
		podDeletionFilter:        podDeletionFilter,
		nodesInProgress:          NewStringSet(),
		eventRecorder:            eventRecorder,
	}

	return mgr
}
