package httpapi

import (
	"net/http"

	"example.com/paleo-prep-station/internal/contracts"
)

func (api *API) registerEvidence(w http.ResponseWriter, r *http.Request) {
	var request contracts.RegisterEvidenceRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	value, err := api.service.RegisterEvidence(r.Context(), request)
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Location", "/evidence/"+value.ID)
	writeJSON(w, http.StatusCreated, value)
}

func (api *API) getEvidence(w http.ResponseWriter, r *http.Request) {
	value, err := api.service.GetEvidence(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (api *API) listEvidenceForSpecimen(w http.ResponseWriter, r *http.Request) {
	values, err := api.service.ListEvidenceForSpecimen(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": values})
}

func (api *API) listEvidenceForPlan(w http.ResponseWriter, r *http.Request) {
	values, err := api.service.ListEvidenceForPlan(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": values})
}
