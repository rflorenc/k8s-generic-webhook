package webhook

import (
	"context"
	"encoding/json"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/runtime/inject"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// Handler is an interface that combines various interfaces.
type Handler interface {
	admission.Handler
	admission.DecoderInjector
	inject.Client
}

// handler is wrapper type for Validator and Mutator, implements the Handler interface.
type handler struct {
	Handler interface{}
	Object  runtime.Object

	decoder *admission.Decoder
}

// Handle implements the admission.Handler interface.
func (h *handler) Handle(ctx context.Context, req admission.Request) admission.Response {
	// add metadata to context's logger
	logger := log.FromContext(ctx).
		WithValues("name", req.Name).
		WithValues("namespace", req.Namespace).
		WithValues("gvk", req.Kind.String()).
		WithValues("uid", req.UID)
	ctx = log.IntoContext(ctx, logger)

	// decode object
	if len(req.Object.Raw) > 0 && req.Object.Object == nil {
		obj := h.Object.DeepCopyObject()
		if err := h.decoder.DecodeRaw(req.Object, obj); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}

		req.Object.Object = obj
	}

	// decode old object
	if len(req.OldObject.Raw) > 0 && req.OldObject.Object == nil {
		obj := h.Object.DeepCopyObject()
		if err := h.decoder.DecodeRaw(req.OldObject, obj); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}

		req.OldObject.Object = obj
	}

	// invoke validator
	if validator, ok := h.Handler.(Validator); ok {
		switch req.Operation {
		case admissionv1.Create:
			return validator.ValidateCreate(ctx, req, req.Object.Object)
		case admissionv1.Update:
			return validator.ValidateUpdate(ctx, req, req.Object.Object, req.OldObject.Object)
		case admissionv1.Delete:
			return validator.ValidateDelete(ctx, req, req.OldObject.Object)
		}
	}

	// invoke mutator
	if mutator, ok := h.Handler.(Mutator); ok {
		if req.Object.Object != nil {
			resp := mutator.Mutate(ctx, req, req.Object.Object)
			if resp.Allowed && resp.Patches == nil {
				// generate patches
				marshalled, err := json.Marshal(req.Object.Object)
				if err != nil {
					return admission.Errored(http.StatusInternalServerError, err)
				}

				return admission.PatchResponseFromRaw(req.Object.Raw, marshalled)
			}

			return resp
		} else {
			return admission.Allowed("")
		}
	}

	return admission.Denied("")
}

// InjectDecoder implements the admission.DecoderInjector interface.
func (h *handler) InjectDecoder(decoder *admission.Decoder) error {
	h.decoder = decoder

	// pass decoder to the underlying handler
	if injector, ok := h.Handler.(admission.DecoderInjector); ok {
		return injector.InjectDecoder(decoder)
	}

	return nil
}

// InjectClient implements the inject.Client interface.
func (h *handler) InjectClient(client client.Client) error {
	// pass client to the underlying handler
	if injector, ok := h.Handler.(inject.Client); ok {
		return injector.InjectClient(client)
	}

	return nil
}
