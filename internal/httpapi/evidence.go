package httpapi

import (
	"net/http"
	"strconv"

	"example.com/paleo-prep-station/internal/contracts"
)

func (api *API) registerSpecimenEvidence(w http.ResponseWriter, r *http.Request) {
	var request contracts.RegisterEvidenceRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	value, err := api.service.RegisterSpecimenEvidence(r.Context(), r.PathValue("id"), request)
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Location", "/evidence/"+value.ID)
	writeJSON(w, http.StatusCreated, value)
}

func (api *API) registerPlanEvidence(w http.ResponseWriter, r *http.Request) {
	var request contracts.RegisterEvidenceRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	value, err := api.service.RegisterPlanEvidence(r.Context(), r.PathValue("id"), request)
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Location", "/evidence/"+value.ID)
	writeJSON(w, http.StatusCreated, value)
}

func (api *API) registerProgressEvidence(w http.ResponseWriter, r *http.Request) {
	sequence, err := strconv.Atoi(r.PathValue("seq"))
	if err != nil || sequence <= 0 {
		writeError(w, contracts.Invalid("invalid_progress_sequence", "进度序号必须是正整数"))
		return
	}
	var request contracts.RegisterEvidenceRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	value, err := api.service.RegisterProgressEvidence(r.Context(), r.PathValue("id"), sequence, request)
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Location", "/evidence/"+value.ID)
	writeJSON(w, http.StatusCreated, value)
}

func (api *API) listSpecimenEvidence(w http.ResponseWriter, r *http.Request) {
	values, err := api.service.ListEvidenceForSpecimen(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": values})
}

func (api *API) getEvidence(w http.ResponseWriter, r *http.Request) {
	value, err := api.service.GetEvidence(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (api *API) verifyEvidence(w http.ResponseWriter, r *http.Request) {
	var request contracts.VerifyEvidenceRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	value, err := api.service.VerifyEvidence(r.Context(), r.PathValue("id"), request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}
