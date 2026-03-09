// Package utils provides utility functions for the xkeen-ui application.
package utils

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Error definitions for path validation
var (
	ErrPathTraversal  = errors.New("path traversal attempt detected")
	ErrPathOutsideRoot = errors.New("path outside allowed root")
	ErrSymlinkDetected = errors.New("symlink detected in path")
	ErrEmptyPath       = errors.New("empty path provided")
	ErrInvalidPath     = errors.New("invalid path")
)

// PathValidator validates paths against allowed roots to prevent traversal attacks.
type PathValidator struct {
	allowedRoots  []string
	allowSymlinks bool
	resolvedRoots []string
}

// PathValidatorOption is a functional option for configuring PathValidator
type PathValidatorOption func(*PathValidator)

// WithSymlinks allows symlinks within allowed roots
func WithSymlinks(allow bool) PathValidatorOption {
	return func(pv *PathValidator) {
		pv.allowSymlinks = allow
	}
}

// NewPathValidator creates a new PathValidator with the given allowed roots.
func NewPathValidator(allowedRoots []string, opts ...PathValidatorOption) (*PathValidator, error) {
	if len(allowedRoots) == 0 {
		return nil, errors.New("at least one allowed root is required")
	}

	cleaned := make([]string, 0, len(allowedRoots))
	resolved := make([]string, 0, len(allowedRoots))

	for _, root := range allowedRoots {
		cleanedRoot := filepath.Clean(root)
		absRoot, err := filepath.Abs(cleanedRoot)
		if err != nil {
			return nil, err
		}
		cleaned = append(cleaned, absRoot)

		resolvedRoot, err := filepath.EvalSymlinks(absRoot)
		if err != nil {
			if os.IsNotExist(err) {
				resolved = append(resolved, absRoot)
			} else {
				return nil, err
			}
		} else {
			resolved = append(resolved, resolvedRoot)
		}
	}

	pv := &PathValidator{
		allowedRoots:  cleaned,
		resolvedRoots: resolved,
		allowSymlinks: false,
	}

	for _, opt := range opts {
		opt(pv)
	}

	return pv, nil
}

// Validate checks if the given path is within the allowed roots.
func (pv *PathValidator) Validate(path string) (string, error) {
	if path == "" {
		return "", ErrEmptyPath
	}

	cleaned := filepath.Clean(path)

	// Check for traversal patterns
	if containsTraversalPattern(cleaned) {
		return "", ErrPathTraversal
	}

	absPath, err := filepath.Abs(cleaned)
	if err != nil {
		return "", ErrInvalidPath
	}

	// Check for symlinks if not allowed
	if !pv.allowSymlinks {
		if err := pv.checkForSymlinks(absPath); err != nil {
			return "", err
		}
	}

	// Resolve path
	resolvedPath := absPath
	if pv.allowSymlinks {
		resolved, err := filepath.EvalSymlinks(absPath)
		if err != nil {
			if os.IsNotExist(err) {
				return pv.validateNonExistentPath(absPath)
			}
			return "", ErrInvalidPath
		}
		resolvedPath = resolved
	}

	// Check against allowed roots
	for i, root := range pv.allowedRoots {
		resolvedRoot := pv.resolvedRoots[i]
		if isWithinRoot(resolvedPath, root) || isWithinRoot(resolvedPath, resolvedRoot) {
			return absPath, nil
		}
	}

	return "", ErrPathOutsideRoot
}

// validateNonExistentPath validates a path that doesn't exist yet
func (pv *PathValidator) validateNonExistentPath(absPath string) (string, error) {
	currentPath := absPath
	for {
		parent := filepath.Dir(currentPath)
		if parent == currentPath {
			return "", ErrPathOutsideRoot
		}

		if _, err := os.Stat(parent); err == nil {
			resolved, err := filepath.EvalSymlinks(parent)
			if err != nil {
				return "", ErrInvalidPath
			}

			for i, root := range pv.allowedRoots {
				resolvedRoot := pv.resolvedRoots[i]
				if isWithinRoot(resolved, root) || isWithinRoot(resolved, resolvedRoot) {
					return absPath, nil
				}
			}
			return "", ErrPathOutsideRoot
		}

		currentPath = parent
	}
}

// IsAllowed checks if the path is allowed without returning the cleaned path.
func (pv *PathValidator) IsAllowed(path string) bool {
	_, err := pv.Validate(path)
	return err == nil
}

// AllowedRoots returns a copy of the allowed roots.
func (pv *PathValidator) AllowedRoots() []string {
	result := make([]string, len(pv.allowedRoots))
	copy(result, pv.allowedRoots)
	return result
}

// checkForSymlinks checks if any component of the path is a symlink.
func (pv *PathValidator) checkForSymlinks(absPath string) error {
	for i, root := range pv.allowedRoots {
		resolvedRoot := pv.resolvedRoots[i]

		if !strings.HasPrefix(absPath, root) && !strings.HasPrefix(absPath, resolvedRoot) {
			continue
		}

		current := root
		rel, err := filepath.Rel(root, absPath)
		if err != nil {
			continue
		}

		components := strings.Split(rel, string(filepath.Separator))

		for _, comp := range components {
			if comp == "" || comp == "." {
				continue
			}

			next := filepath.Join(current, comp)

			fi, err := os.Lstat(next)
			if err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				return ErrInvalidPath
			}

			if fi.Mode()&os.ModeSymlink != 0 {
				return ErrSymlinkDetected
			}

			current = next
		}

		return nil
	}

	return ErrPathOutsideRoot
}

// containsTraversalPattern checks if the path contains traversal patterns.
func containsTraversalPattern(path string) bool {
	parts := strings.Split(path, string(filepath.Separator))
	for _, part := range parts {
		if part == ".." {
			return true
		}
	}
	return false
}

// isWithinRoot checks if a path is within the given root directory.
func isWithinRoot(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)

	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}

	return !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)
}

// CleanPath safely cleans a path without validation.
func CleanPath(path string) string {
	return filepath.Clean(path)
}

// JoinPath safely joins path elements.
func JoinPath(elements ...string) string {
	return filepath.Join(elements...)
}

// ValidateFilename validates a filename (not a path) for safety.
func ValidateFilename(filename string) error {
	if filename == "" {
		return ErrEmptyPath
	}

	if strings.ContainsAny(filename, `/\`) {
		return ErrPathTraversal
	}

	if filename == "." || filename == ".." || strings.Contains(filename, "..") {
		return ErrPathTraversal
	}

	if strings.ContainsRune(filename, '\x00') {
		return ErrInvalidPath
	}

	return nil
}
