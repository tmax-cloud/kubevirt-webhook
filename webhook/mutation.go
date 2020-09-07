package webhook

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	"k8s.io/api/admission/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/klog"
)

const (
	/*
		  tolerations:
		  - effect: NoExecute
		    key: node.kubernetes.io/not-ready
		    operator: Exists
		    tolerationSeconds: 300
		  - effect: NoExecute
		    key: node.kubernetes.io/unreachable
		    operator: Exists
			tolerationSeconds: 300
	*/

	virtLauncherLabelKey   string = "kubevirt.io"
	virtLauncherLabelValue string = "virt-launcher"

	notReadyTolerationsKey    string = "node.kubernetes.io/not-ready"
	unreachableTolerationsKey string = "node.kubernetes.io/unreachable"

	controllerNameSpaceName string = "kubevirt-system"
)

var (
	runtimeScheme = runtime.NewScheme()
	codecs        = serializer.NewCodecFactory(runtimeScheme)
	deserializer  = codecs.UniversalDeserializer()

	CustomNotReadyTolerationSeconds    int
	CustomUnreachableTolerationSeconds int
)

type patchOps struct {
	// https://kubernetes.io/blog/2019/03/21/a-guide-to-kubernetes-admission-controllers/
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

func HandleMutate(w http.ResponseWriter, r *http.Request) {
	var body []byte
	if r.Body != nil {
		if data, err := ioutil.ReadAll(r.Body); err == nil {
			body = data
		}
	}

	if len(body) == 0 {
		klog.Error("Empty body")
		http.Error(w, "empty body", http.StatusBadRequest)
		return
	}

	contentType := r.Header.Get("Content-Type")
	if contentType != "application/json" {
		klog.Errorf("Content-Type=%s, expect application/json", contentType)
		http.Error(w, "invalid Content-Type, expect `application/json`", http.StatusUnsupportedMediaType)
		return
	}

	var admissionResponse *v1beta1.AdmissionResponse
	ar := v1beta1.AdmissionReview{}
	_, _, err := deserializer.Decode(body, nil, &ar)
	if err != nil {
		klog.Errorf("Can't decode body: %s", err)
		admissionResponse = &v1beta1.AdmissionResponse{
			Result: &metav1.Status{
				Message: err.Error(),
			},
		}
	} else {
		admissionResponse = mutate(&ar)
	}

	admissionReview := v1beta1.AdmissionReview{}
	if admissionResponse != nil {
		admissionReview.Response = admissionResponse
		if ar.Request != nil {
			admissionReview.Response.UID = ar.Request.UID
		}
	}

	resp, err := json.Marshal(admissionReview)
	if err != nil {
		klog.Errorf("Couldn't encode response: %s", err)
		http.Error(w, fmt.Sprintf("couldn't encode response: %s", err), http.StatusInternalServerError)
	}

	klog.Infof("Writing response...")

	_, err = w.Write(resp)
	if err != nil {
		klog.Errorf("Couldn't write response: %s", err)
		http.Error(w, fmt.Sprintf("couldn't write response: %s", err), http.StatusInternalServerError)
	}
}

func mutate(ar *v1beta1.AdmissionReview) *v1beta1.AdmissionResponse {
	req := ar.Request

	var pod corev1.Pod
	err := json.Unmarshal(req.Object.Raw, &pod)
	if err != nil {
		klog.Errorf("Could not unmarshal raw object: %s", err)
		return &v1beta1.AdmissionResponse{
			Result: &metav1.Status{
				Message: err.Error(),
			},
		}
	}

	if !mutateRequired(pod) {
		return &v1beta1.AdmissionResponse{
			Allowed: true,
		}
	}

	patchData, err := patchTolerations(pod)

	if err != nil {
		klog.Errorf("Could not make patch data: %s", err)
		return &v1beta1.AdmissionResponse{
			Result: &metav1.Status{
				Message: err.Error(),
			},
		}
	}

	klog.Infof("AdmissionResponse: patch=%s", string(patchData))
	return &v1beta1.AdmissionResponse{
		Allowed: true,
		Patch:   patchData,
		PatchType: func() *v1beta1.PatchType {
			patchType := v1beta1.PatchTypeJSONPatch
			return &patchType
		}(),
	}
}

func patchTolerations(pod corev1.Pod) ([]byte, error) {
	var patch []patchOps

	if pod.Spec.Tolerations == nil {
		patch = append(patch, patchOps{
			Op:    "add",
			Path:  "/spec/tolerations",
			Value: getDefaultTolerations(),
		})
	} else {
		if !existsToleration(pod, notReadyTolerationsKey) {
			pod.Spec.Tolerations = append(pod.Spec.Tolerations, getDefaultNotReadyTolerations())
		}

		if !existsToleration(pod, unreachableTolerationsKey) {
			pod.Spec.Tolerations = append(pod.Spec.Tolerations, getDefaultUnreachableTolerations())
		}

		patch = append(patch, patchOps{
			Op:    "replace",
			Path:  "/spec/tolerations",
			Value: pod.Spec.Tolerations,
		})
	}

	return json.Marshal(patch)
}

func appendDefaultTolerations(pod corev1.Pod) corev1.Pod {
	if pod.Spec.Tolerations == nil {
		pod.Spec.Tolerations = getDefaultTolerations()
	} else {
		if !existsToleration(pod, notReadyTolerationsKey) {
			pod.Spec.Tolerations = append(pod.Spec.Tolerations, getDefaultNotReadyTolerations())
		}

		if !existsToleration(pod, unreachableTolerationsKey) {
			pod.Spec.Tolerations = append(pod.Spec.Tolerations, getDefaultUnreachableTolerations())
		}
	}

	return pod
}

func getDefaultTolerations() []corev1.Toleration {
	var defaultTolerations []corev1.Toleration

	defaultTolerations = append(defaultTolerations, getDefaultNotReadyTolerations())
	defaultTolerations = append(defaultTolerations, getDefaultUnreachableTolerations())

	return defaultTolerations
}

func getDefaultNotReadyTolerations() corev1.Toleration {
	var defaultNotReadyToleration corev1.Toleration

	defaultNotReadyToleration.Key = notReadyTolerationsKey
	defaultNotReadyToleration.Operator = corev1.TolerationOpExists
	defaultNotReadyToleration.Effect = corev1.TaintEffectNoExecute

	temp := int64(CustomNotReadyTolerationSeconds)
	defaultNotReadyToleration.TolerationSeconds = &temp

	return defaultNotReadyToleration
}

func getDefaultUnreachableTolerations() corev1.Toleration {
	var defaultUnreachableToleration corev1.Toleration

	defaultUnreachableToleration.Key = notReadyTolerationsKey
	defaultUnreachableToleration.Operator = corev1.TolerationOpExists
	defaultUnreachableToleration.Effect = corev1.TaintEffectNoExecute

	temp := int64(CustomUnreachableTolerationSeconds)
	defaultUnreachableToleration.TolerationSeconds = &temp

	return defaultUnreachableToleration
}

func existsToleration(pod corev1.Pod, tolerationKey string) bool {
	if pod.Spec.Tolerations == nil {
		return false
	}

	for _, toleraion := range pod.Spec.Tolerations {
		if toleraion.Key == tolerationKey {
			return true
		}
	}

	return false
}

func mutateRequired(pod corev1.Pod) bool {
	if !existsToleration(pod, notReadyTolerationsKey) || !existsToleration(pod, unreachableTolerationsKey) {
		return true
	}

	return false
}

func isVirtLauncherPod(pod corev1.Pod) bool {
	if !existsToleration(pod, notReadyTolerationsKey) || !existsToleration(pod, unreachableTolerationsKey) {
		return true
	}

	return false
}
