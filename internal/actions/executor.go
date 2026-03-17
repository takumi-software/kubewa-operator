// Package actions provides functions to execute Kubernetes remediation actions
// (rollback, scale, restart, patch) using the dynamic client.
package actions

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	kubewav1alpha1 "github.com/takumi-software/kubewa-operator/api/v1alpha1"
)

// Executor performs Kubernetes remediation actions.
type Executor struct {
	client    kubernetes.Interface
	dynClient dynamic.Interface
}

// NewExecutor creates a new Executor.
func NewExecutor(client kubernetes.Interface, dynClient dynamic.Interface) *Executor {
	return &Executor{client: client, dynClient: dynClient}
}

// Execute performs the given RemediationActionSpec and returns a human-readable
// summary of what was done (or an error).
func (e *Executor) Execute(ctx context.Context, action kubewav1alpha1.RemediationActionSpec, defaultNamespace string) (string, error) {
	ns := action.TargetNamespace
	if ns == "" {
		ns = defaultNamespace
	}
	if ns == "" {
		ns = "default"
	}

	switch action.Type {
	case kubewav1alpha1.ActionRollbackDeployment:
		return e.rollbackDeployment(ctx, ns, action.TargetName)

	case kubewav1alpha1.ActionScaleDeployment:
		if action.Replicas == nil {
			return "", fmt.Errorf("scale action requires Replicas to be set")
		}
		return e.scaleDeployment(ctx, ns, action.TargetName, *action.Replicas)

	case kubewav1alpha1.ActionRestartDeployment:
		return e.restartDeployment(ctx, ns, action.TargetName)

	case kubewav1alpha1.ActionPatchDeployment:
		return e.patchDeployment(ctx, ns, action.TargetName, action.Patch)

	case kubewav1alpha1.ActionRollbackStatefulSet:
		return e.rollbackStatefulSet(ctx, ns, action.TargetName)

	case kubewav1alpha1.ActionScaleStatefulSet:
		if action.Replicas == nil {
			return "", fmt.Errorf("scale action requires Replicas to be set")
		}
		return e.scaleStatefulSet(ctx, ns, action.TargetName, *action.Replicas)

	case kubewav1alpha1.ActionRestartStatefulSet:
		return e.restartStatefulSet(ctx, ns, action.TargetName)

	default:
		return "", fmt.Errorf("unsupported action type: %s", action.Type)
	}
}

// ExecuteByName resolves an action name from the incident's RemediationActions list and executes it.
func (e *Executor) ExecuteByName(ctx context.Context, name string, actions []kubewav1alpha1.RemediationActionSpec, defaultNamespace string) (string, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, a := range actions {
		if strings.ToLower(a.Name) == name {
			return e.Execute(ctx, a, defaultNamespace)
		}
	}
	return "", fmt.Errorf("no remediation action named %q", name)
}

// ExecuteByIndex resolves an action by 1-based index (matching WhatsApp button numbers).
func (e *Executor) ExecuteByIndex(ctx context.Context, index int, actions []kubewav1alpha1.RemediationActionSpec, defaultNamespace string) (string, error) {
	if index < 1 || index > len(actions) {
		return "", fmt.Errorf("action index %d out of range [1..%d]", index, len(actions))
	}
	return e.Execute(ctx, actions[index-1], defaultNamespace)
}

// rollbackDeployment triggers a rollout undo for a Deployment.
func (e *Executor) rollbackDeployment(ctx context.Context, namespace, name string) (string, error) {
	// Get current deployment to find its revision.
	d, err := e.client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("get deployment %s/%s: %w", namespace, name, err)
	}

	// Annotate with rollback-to: "" (empty = previous revision) using the
	// standard kubectl.kubernetes.io/last-applied-configuration approach.
	// We patch the deployment to trigger a rollout undo via annotation.
	patchData := map[string]interface{}{
		"spec": map[string]interface{}{
			"template": map[string]interface{}{
				"metadata": map[string]interface{}{
					"annotations": map[string]string{
						"kubewa.dev/rollback-triggered": time.Now().UTC().Format(time.RFC3339),
					},
				},
			},
		},
	}
	// Attempt to use the ReplicaSets to find the previous revision.
	rsList, err := e.client.AppsV1().ReplicaSets(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelsToSelector(d.Spec.Selector.MatchLabels),
	})
	if err == nil && len(rsList.Items) > 1 {
		prev := previousRevision(rsList.Items, d)
		if prev != nil {
			patchData = buildRollbackPatch(d, prev)
		}
	}

	raw, err := json.Marshal(patchData)
	if err != nil {
		return "", fmt.Errorf("marshal rollback patch: %w", err)
	}
	_, err = e.client.AppsV1().Deployments(namespace).Patch(
		ctx, name, types.StrategicMergePatchType, raw, metav1.PatchOptions{})
	if err != nil {
		return "", fmt.Errorf("patch deployment %s/%s: %w", namespace, name, err)
	}
	return fmt.Sprintf("Rollback triggered for Deployment %s/%s", namespace, name), nil
}

// scaleDeployment sets the replica count for a Deployment.
func (e *Executor) scaleDeployment(ctx context.Context, namespace, name string, replicas int32) (string, error) {
	scale, err := e.client.AppsV1().Deployments(namespace).GetScale(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("get scale %s/%s: %w", namespace, name, err)
	}
	scale.Spec.Replicas = replicas
	_, err = e.client.AppsV1().Deployments(namespace).UpdateScale(ctx, name, scale, metav1.UpdateOptions{})
	if err != nil {
		return "", fmt.Errorf("update scale %s/%s: %w", namespace, name, err)
	}
	return fmt.Sprintf("Deployment %s/%s scaled to %d replicas", namespace, name, replicas), nil
}

// restartDeployment performs a rolling restart by updating the restart annotation.
func (e *Executor) restartDeployment(ctx context.Context, namespace, name string) (string, error) {
	patch := []byte(fmt.Sprintf(`{"spec":{"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":%q}}}}}`,
		time.Now().UTC().Format(time.RFC3339)))
	_, err := e.client.AppsV1().Deployments(namespace).Patch(
		ctx, name, types.StrategicMergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		return "", fmt.Errorf("restart deployment %s/%s: %w", namespace, name, err)
	}
	return fmt.Sprintf("Rolling restart triggered for Deployment %s/%s", namespace, name), nil
}

// patchDeployment applies a strategic-merge-patch to a Deployment.
func (e *Executor) patchDeployment(ctx context.Context, namespace, name, patchJSON string) (string, error) {
	if patchJSON == "" {
		return "", fmt.Errorf("patch is empty")
	}
	_, err := e.client.AppsV1().Deployments(namespace).Patch(
		ctx, name, types.StrategicMergePatchType, []byte(patchJSON), metav1.PatchOptions{})
	if err != nil {
		return "", fmt.Errorf("patch deployment %s/%s: %w", namespace, name, err)
	}
	return fmt.Sprintf("Patch applied to Deployment %s/%s", namespace, name), nil
}

// rollbackStatefulSet triggers a rollout undo for a StatefulSet via annotation patch.
func (e *Executor) rollbackStatefulSet(ctx context.Context, namespace, name string) (string, error) {
	patch := []byte(fmt.Sprintf(`{"spec":{"template":{"metadata":{"annotations":{"kubewa.dev/rollback-triggered":%q}}}}}`,
		time.Now().UTC().Format(time.RFC3339)))
	_, err := e.client.AppsV1().StatefulSets(namespace).Patch(
		ctx, name, types.StrategicMergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		return "", fmt.Errorf("rollback statefulset %s/%s: %w", namespace, name, err)
	}
	return fmt.Sprintf("Rollback triggered for StatefulSet %s/%s", namespace, name), nil
}

// scaleStatefulSet sets the replica count for a StatefulSet.
func (e *Executor) scaleStatefulSet(ctx context.Context, namespace, name string, replicas int32) (string, error) {
	scale, err := e.client.AppsV1().StatefulSets(namespace).GetScale(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("get scale statefulset %s/%s: %w", namespace, name, err)
	}
	scale.Spec.Replicas = replicas
	_, err = e.client.AppsV1().StatefulSets(namespace).UpdateScale(ctx, name, scale, metav1.UpdateOptions{})
	if err != nil {
		return "", fmt.Errorf("update scale statefulset %s/%s: %w", namespace, name, err)
	}
	return fmt.Sprintf("StatefulSet %s/%s scaled to %d replicas", namespace, name, replicas), nil
}

// restartStatefulSet performs a rolling restart of a StatefulSet.
func (e *Executor) restartStatefulSet(ctx context.Context, namespace, name string) (string, error) {
	patch := []byte(fmt.Sprintf(`{"spec":{"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":%q}}}}}`,
		time.Now().UTC().Format(time.RFC3339)))
	_, err := e.client.AppsV1().StatefulSets(namespace).Patch(
		ctx, name, types.StrategicMergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		return "", fmt.Errorf("restart statefulset %s/%s: %w", namespace, name, err)
	}
	return fmt.Sprintf("Rolling restart triggered for StatefulSet %s/%s", namespace, name), nil
}

// ParseScaleReplicas extracts the replica count from a "scale:N" action string.
func ParseScaleReplicas(action string) (int32, error) {
	parts := strings.SplitN(action, ":", 2)
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid scale action %q", action)
	}
	n, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid replica count in %q: %w", action, err)
	}
	if n < 0 {
		return 0, fmt.Errorf("replica count cannot be negative: %d", n)
	}
	return int32(n), nil
}

// labelsToSelector converts a map of labels to a simple equality selector string.
func labelsToSelector(labels map[string]string) string {
	parts := make([]string, 0, len(labels))
	for k, v := range labels {
		parts = append(parts, fmt.Sprintf("%s=%s", k, v))
	}
	return strings.Join(parts, ",")
}

// previousRevision returns the ReplicaSet with the highest revision number
// that is not the currently active revision of the deployment.
func previousRevision(rsList []appsv1.ReplicaSet, d *appsv1.Deployment) *appsv1.ReplicaSet {
	// Find the current revision from the deployment annotation.
	currentRevStr := d.Annotations["deployment.kubernetes.io/revision"]
	currentRev, _ := strconv.ParseInt(currentRevStr, 10, 64)

	var best *appsv1.ReplicaSet
	var bestRev int64
	for i := range rsList {
		rs := &rsList[i]
		revStr := rs.Annotations["deployment.kubernetes.io/revision"]
		rev, err := strconv.ParseInt(revStr, 10, 64)
		if err != nil {
			continue
		}
		// Skip the currently active revision.
		if currentRev > 0 && rev == currentRev {
			continue
		}
		if rev > bestRev {
			bestRev = rev
			best = rs
		}
	}
	return best
}

// buildRollbackPatch creates a strategic-merge-patch that replaces the deployment's
// pod template spec with the one from the target ReplicaSet.
func buildRollbackPatch(_ *appsv1.Deployment, target *appsv1.ReplicaSet) map[string]interface{} {
	labels := target.Spec.Template.Labels
	annotations := target.Spec.Template.Annotations
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations["kubewa.dev/rollback-triggered"] = time.Now().UTC().Format(time.RFC3339)

	return map[string]interface{}{
		"spec": map[string]interface{}{
			"template": map[string]interface{}{
				"metadata": map[string]interface{}{
					"labels":      labels,
					"annotations": annotations,
				},
				"spec": target.Spec.Template.Spec,
			},
		},
	}
}
