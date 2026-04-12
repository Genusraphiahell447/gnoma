package plugin

import "errors"

var (
	ErrManifestNotFound = errors.New("plugin: plugin.json not found")
	ErrManifestInvalid  = errors.New("plugin: invalid manifest")
	ErrAlreadyInstalled = errors.New("plugin: already installed")
	ErrNotFound         = errors.New("plugin: not found")
	ErrVersionMismatch  = errors.New("plugin: gnoma version does not satisfy constraint")
	ErrPathTraversal    = errors.New("plugin: path traversal detected")
)
