package main

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"strconv"
)

// Router is the HTTP router for handling API requests related to pull requests and Docker operations.
type Router struct {
	docker *Docker
	apiKey string

	mux *http.ServeMux
}

// NewRouter creates a new Router instance with the provided Docker client and API key. If the API key is
// empty, it will not enforce API key authentication for the routes.
func NewRouter(docker *Docker, apiKey string) *Router {
	return &Router{
		docker: docker,
		apiKey: apiKey,

		mux: http.NewServeMux(),
	}
}

// Run starts the HTTP server on the specified address. It sets up the routes for creating and deleting
// pull requests, applying the API key middleware if an API key is provided.
func (r *Router) Run(addr string) error {
	slog.Info("Starting API server", "addr", addr)
	r.mux.Handle("POST /pullrequest", r.apiKeyMiddleware(http.HandlerFunc(r.handleCreatePullRequest)))
	r.mux.Handle("DELETE /pullrequest/{pr}", r.apiKeyMiddleware(http.HandlerFunc(r.handleDeletePullRequest)))
	return http.ListenAndServe(addr, r.mux)
}

// apiKeyMiddleware is a middleware that checks for the presence of a valid API key in the request headers.
func (r *Router) apiKeyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		apiKey := request.Header.Get("X-API-Key")
		if apiKey != r.apiKey {
			slog.Warn("Invalid API key", "provided_key", apiKey)
			http.Error(writer, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(writer, request)
	})
}

// handleCreatePullRequest handles the creation of a new pull request by uploading a binary file and building
// a Docker image.
func (r *Router) handleCreatePullRequest(writer http.ResponseWriter, request *http.Request) {
	logger := slog.Default().With(slog.Group(
		"request",
		slog.String("method", request.Method),
		slog.String("url", request.URL.String()),
	))

	// Try to parse the multipart form data from the request to extract the PR number and binary file.
	if err := request.ParseMultipartForm(10 << 20); err != nil {
		logger.Warn("Failed to parse form", slog.Any("error", err))
		http.Error(writer, "Failed to parse form", http.StatusBadRequest)
		return
	}
	pr := request.FormValue("pr")
	if _, err := strconv.Atoi(pr); err != nil {
		logger.Warn("Invalid PR number", "pr", pr, slog.Any("error", err))
		http.Error(writer, "Invalid PR number", http.StatusBadRequest)
		return
	}
	file, _, err := request.FormFile("binary")
	if err != nil {
		logger.Warn("Failed to get file from form", slog.Any("error", err))
		http.Error(writer, "Failed to get file from form", http.StatusBadRequest)
		return
	}

	// Upload the binary file and build the Docker image for the PR.
	if err = uploadBinary(pr, file); err != nil {
		logger.Error("Failed to upload binary", "pr", pr, slog.Any("error", err))
		http.Error(writer, fmt.Sprintf("Failed to upload binary: %v", err), http.StatusInternalServerError)
		return
	}
	if err = r.docker.BuildImage(pr); err != nil {
		logger.Error("Failed to build image", "pr", pr, slog.Any("error", err))
		http.Error(writer, fmt.Sprintf("Failed to build image: %v", err), http.StatusInternalServerError)
		return
	}

	logger.Info("Successfully uploaded PR", "pr", pr)
	writer.WriteHeader(http.StatusCreated)
}

// handleDeletePullRequest handles the deletion of a pull request by removing the associated files and
// stopping the Docker container.
func (r *Router) handleDeletePullRequest(writer http.ResponseWriter, request *http.Request) {
	logger := slog.Default().With(slog.Group(
		"request",
		slog.String("method", request.Method),
		slog.String("url", request.URL.String()),
	))

	// Extract the PR number from the request path.
	pr := request.PathValue("pr")
	if _, err := strconv.Atoi(pr); err != nil {
		logger.Warn("Invalid PR number", "pr", pr, slog.Any("error", err))
		http.Error(writer, "Invalid PR number", http.StatusBadRequest)
		return
	}

	// Check if the PR actually exists before attempting to delete it.
	name := "pr-" + pr
	_, err := os.Stat(name)
	if errors.Is(err, os.ErrNotExist) {
		logger.Warn("PR not found", "pr", pr)
		http.Error(writer, "PR not found", http.StatusNotFound)
		return
	}

	// Delete the server from Docker and remove the associated files.
	r.docker.DeleteServer(pr)
	_ = os.RemoveAll("pr-" + pr)
	_ = os.Remove("binaries/pr-" + pr)

	logger.Info("Successfully deleted PR", "pr", pr)
	writer.WriteHeader(http.StatusNoContent)
}

// uploadBinary uploads the binary file for the specified pull request (PR) number. It creates a directory for
// the PR server's save data to later mount to.
func uploadBinary(pr string, file multipart.File) error {
	_ = os.Mkdir("pr-"+pr, 0755)
	out, err := os.Create("binaries/pr-" + pr)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer out.Close()
	if _, err := file.Seek(0, 0); err != nil {
		return fmt.Errorf("failed to seek file: %w", err)
	}
	if _, err := io.Copy(out, file); err != nil {
		return fmt.Errorf("failed to copy file: %w", err)
	}
	return nil
}
