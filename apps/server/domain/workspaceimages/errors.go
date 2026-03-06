package workspaceimages

import "errors"

var (
	// ErrNameRequired is returned when the image name is empty.
	ErrNameRequired = errors.New("image name is required")

	// ErrNameConflict is returned when an image with the same name already exists in the project.
	ErrNameConflict = errors.New("an image with this name already exists in the project")

	// ErrNotFound is returned when the requested image does not exist.
	ErrNotFound = errors.New("workspace image not found")

	// ErrBuiltInImmutable is returned when attempting to modify or delete a built-in image.
	ErrBuiltInImmutable = errors.New("built-in images cannot be modified or deleted")
)
