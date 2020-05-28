package main

import (
	"encoding/json"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"k8s.io/api/admission/v1beta1"
)

func (s *nsmAdmissionWebhook) mutate(request *v1beta1.AdmissionRequest) *v1beta1.AdmissionResponse {
	var dp_name, devices string
	logrus.Infof("AdmissionReview for =%v", request)
	if !isSupportKind(request) {
		return okReviewResponse()
	}
	metaAndSpec, err := getMetaAndSpec(request)
	if err != nil {
		return errorReviewResponse(err)
	}
	value, dp_value, dp_ok, ok := getNsmAnnotationValue(ignoredNamespaces, metaAndSpec)
	if !ok {
		logrus.Infof("Skipping validation for %s/%s due to policy check", metaAndSpec.meta.Namespace, metaAndSpec.meta.Name)
		return okReviewResponse()
	}
	if dp_ok {
		if dp_value == "" {
			err = errors.New("Device pool value cannot be empty")
			return errorReviewResponse(err)
		}
		dp_name, devices, err = parseDpAnnotationValue(dp_value)
		if err != nil {
			return errorReviewResponse(err)
		}
	}
	if err = validateAnnotationValue(value); err != nil {
		return errorReviewResponse(err)
	}
	if err = checkNsmInitContainerDuplication(metaAndSpec.spec); err != nil {
		return errorReviewResponse(err)
	}
	patch := createNsmInitContainerPatch(metaAndSpec.spec.InitContainers, value, dp_name, devices)
	patch = append(patch, createDNSPatch(metaAndSpec, value)...)
	//append another patches
	applyDeploymentKind(patch, request.Kind.Kind)
	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return errorReviewResponse(err)
	}
	logrus.Infof("AdmissionResponse: patch=%v\n", string(patchBytes))

	return createReviewResponse(patchBytes)
}
