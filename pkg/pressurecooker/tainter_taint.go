package pressurecooker

import (
	"fmt"

	"github.com/golang/glog"
	"github.com/rtreffer/kubernetes-pressurecooker/pkg/jsonpatch"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func (t *Tainter) IsNodeTainted() (bool, error) {
	node, err := t.client.CoreV1().Nodes().Get(t.nodeName, metav1.GetOptions{})
	if err != nil {
		return false, err
	}

	for i := range node.Spec.Taints {
		if node.Spec.Taints[i].Key == TaintKey {
			return true, nil
		}
	}

	return false, nil
}

func (t *Tainter) IsPressurecookerDisabled() (bool, error) {
	node, err := t.client.CoreV1().Nodes().Get(t.nodeName, metav1.GetOptions{})
	if err != nil {
		return false, err
	}

	for k, v := range node.Labels {
		if k != "pressurecooker.enabled" {
			continue
		}
		if v == "FALSE" || v == "false" {
			return true, nil
		}
	}

	return false, nil
}

func (t *Tainter) TaintNode(evt ThresholdEvent) error {
	node, err := t.client.CoreV1().Nodes().Get(t.nodeName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	nodeCopy := node.DeepCopy()

	if nodeCopy.Spec.Taints == nil {
		nodeCopy.Spec.Taints = make([]v1.Taint, 0, 1)
	}

	for i := range nodeCopy.Spec.Taints {
		if nodeCopy.Spec.Taints[i].Key == TaintKey {
			glog.Infof("wanted to taint node %s, but taint already exists", nodeCopy.Name)
			return nil
		}
	}

	nodeCopy.Spec.Taints = append(nodeCopy.Spec.Taints, v1.Taint{
		Key:    TaintKey,
		Value:  "true",
		Effect: v1.TaintEffectPreferNoSchedule,
	})

	_, err = t.client.CoreV1().Nodes().Update(nodeCopy)

	t.recorder.Eventf(t.nodeRef, v1.EventTypeWarning, "CPUPressureExceeded", "%s, tainting node", evt.String())

	if err != nil {
		t.recorder.Eventf(t.nodeRef, v1.EventTypeWarning, "NodePatchError", "could not patch node: %s", err.Error())
		return err
	}

	return nil
}

func (t *Tainter) UntaintNode(evt ThresholdEvent) error {
	node, err := t.client.CoreV1().Nodes().Get(t.nodeName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	taintIndex := -1

	for i, t := range node.Spec.Taints {
		if t.Key == TaintKey {
			taintIndex = i
			break
		}
	}

	if taintIndex == -1 {
		glog.Infof("wanted to remove taint from node %s, but taint was already gone", node.Name)
		return nil
	}

	t.recorder.Eventf(t.nodeRef, v1.EventTypeNormal, "LoadThresholdDeceeded", "%s. untainting node", evt.String())

	_, err = t.client.CoreV1().Nodes().Patch(t.nodeName, types.JSONPatchType, jsonpatch.PatchList{{
		Op:    "test",
		Path:  fmt.Sprintf("/spec/taints/%d/key", taintIndex),
		Value: TaintKey,
	}, {
		Op:    "remove",
		Path:  fmt.Sprintf("/spec/taints/%d", taintIndex),
		Value: "",
	}}.ToJSON())

	if err != nil {
		t.recorder.Eventf(t.nodeRef, v1.EventTypeWarning, "NodePatchError", "could not patch node: %s", err.Error())
		return err
	}

	return nil
}
