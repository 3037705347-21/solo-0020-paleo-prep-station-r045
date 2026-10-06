package httpapi

import (
	"net/http"

	"example.com/paleo-prep-station/internal/contracts"
)

func (api *API) releaseSpecimen(w http.ResponseWriter, r *http.Request) {
	var request contracts.ReleaseRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	value, err := api.service.ReleaseSpecimen(r.Context(), r.PathValue("id"), request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, value)
}

func (api *API) getRelease(w http.ResponseWriter, r *http.Request) {
	value, err := api.service.GetRelease(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}
