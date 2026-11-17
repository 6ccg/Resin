package api

import (
	"net/http"

	"github.com/Resinat/Resin/internal/service"
)

func HandleNodeDiagnostics(cp *service.ControlPlaneService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		result, err := cp.GetNodeDiagnostics(PathParam(r, "hash"))
		if err != nil {
			writeServiceError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, result)
	}
}

func HandleCheckNodeTarget(cp *service.ControlPlaneService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request service.CheckTargetRequest
		if err := DecodeBody(r, &request); err != nil {
			writeDecodeBodyError(w, err)
			return
		}
		result, err := cp.CheckNodeTarget(PathParam(r, "hash"), request)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, result)
	}
}
