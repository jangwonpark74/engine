package util

import (
	"encoding/json"
	"fmt"

	"github.com/appscode/go/log"
	"github.com/appscode/kutil"
	jsonpatch "github.com/evanphx/json-patch"
	api "github.com/kube-ci/engine/apis/engine/v1alpha1"
	cs "github.com/kube-ci/engine/client/clientset/versioned/typed/engine/v1alpha1"
	"github.com/pkg/errors"
	kerr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
)

func CreateOrPatchWorkplan(c cs.EngineV1alpha1Interface, meta metav1.ObjectMeta, transform func(workplan *api.Workplan) *api.Workplan) (*api.Workplan, kutil.VerbType, error) {
	cur, err := c.Workplans(meta.Namespace).Get(meta.Name, metav1.GetOptions{})
	if kerr.IsNotFound(err) {
		log.Infof("Creating Workplan %s/%s.", meta.Namespace, meta.Name)
		out, err := c.Workplans(meta.Namespace).Create(transform(&api.Workplan{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Workplan",
				APIVersion: api.SchemeGroupVersion.String(),
			},
			ObjectMeta: meta,
		}))
		return out, kutil.VerbCreated, err
	} else if err != nil {
		return nil, kutil.VerbUnchanged, err
	}
	return PatchWorkplan(c, cur, transform)
}

func PatchWorkplan(c cs.EngineV1alpha1Interface, cur *api.Workplan, transform func(*api.Workplan) *api.Workplan) (*api.Workplan, kutil.VerbType, error) {
	return PatchWorkplanObject(c, cur, transform(cur.DeepCopy()))
}

func PatchWorkplanObject(c cs.EngineV1alpha1Interface, cur, mod *api.Workplan) (*api.Workplan, kutil.VerbType, error) {
	curJson, err := json.Marshal(cur)
	if err != nil {
		return nil, kutil.VerbUnchanged, err
	}

	modJson, err := json.Marshal(mod)
	if err != nil {
		return nil, kutil.VerbUnchanged, err
	}

	patch, err := jsonpatch.CreateMergePatch(curJson, modJson)
	if err != nil {
		return nil, kutil.VerbUnchanged, err
	}
	if len(patch) == 0 || string(patch) == "{}" {
		return cur, kutil.VerbUnchanged, nil
	}
	log.Infof("Patching Workplan %s/%s with %s.", cur.Namespace, cur.Name, string(patch))
	out, err := c.Workplans(cur.Namespace).Patch(cur.Name, types.MergePatchType, patch)
	return out, kutil.VerbPatched, err
}

func TryUpdateWorkplan(c cs.EngineV1alpha1Interface, meta metav1.ObjectMeta, transform func(*api.Workplan) *api.Workplan) (result *api.Workplan, err error) {
	attempt := 0
	err = wait.PollImmediate(kutil.RetryInterval, kutil.RetryTimeout, func() (bool, error) {
		attempt++
		cur, e2 := c.Workplans(meta.Namespace).Get(meta.Name, metav1.GetOptions{})
		if kerr.IsNotFound(e2) {
			return false, e2
		} else if e2 == nil {
			result, e2 = c.Workplans(cur.Namespace).Update(transform(cur.DeepCopy()))
			return e2 == nil, nil
		}
		log.Errorf("Attempt %d failed to update Workplan %s/%s due to %v.", attempt, cur.Namespace, cur.Name, e2)
		return false, nil
	})

	if err != nil {
		err = fmt.Errorf("failed to update Workplan %s/%s after %d attempts due to %v", meta.Namespace, meta.Name, attempt, err)
	}
	return
}

func UpdateWorkplanStatus(
	c cs.EngineV1alpha1Interface,
	in *api.Workplan,
	transform func(status *api.WorkplanStatus) *api.WorkplanStatus,
	useSubresource ...bool,
) (result *api.Workplan, err error) {
	if len(useSubresource) > 1 {
		return nil, errors.Errorf("invalid value passed for useSubresource: %v", useSubresource)
	}
	apply := func(x *api.Workplan) *api.Workplan {
		out := &api.Workplan{
			TypeMeta:   x.TypeMeta,
			ObjectMeta: x.ObjectMeta,
			Spec:       x.Spec,
			Status:     *transform(in.Status.DeepCopy()),
		}
		return out
	}

	if len(useSubresource) == 1 && useSubresource[0] {
		attempt := 0
		cur := in.DeepCopy()
		err = wait.PollImmediate(kutil.RetryInterval, kutil.RetryTimeout, func() (bool, error) {
			attempt++
			var e2 error
			result, e2 = c.Workplans(in.Namespace).UpdateStatus(apply(cur))
			if kerr.IsConflict(e2) {
				latest, e3 := c.Workplans(in.Namespace).Get(in.Name, metav1.GetOptions{})
				switch {
				case e3 == nil:
					cur = latest
					return false, nil
				case kutil.IsRequestRetryable(e3):
					return false, nil
				default:
					return false, e3
				}
			} else if err != nil && !kutil.IsRequestRetryable(e2) {
				return false, e2
			}
			return e2 == nil, nil
		})

		if err != nil {
			err = fmt.Errorf("failed to update status of Workplan %s/%s after %d attempts due to %v", in.Namespace, in.Name, attempt, err)
		}
		return
	}

	result, _, err = PatchWorkplanObject(c, in, apply(in))
	return
}
