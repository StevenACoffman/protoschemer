package protoemit

import (
	"sort"
	"strings"

	"github.com/StevenACoffman/protoschemer/internal/protoir"
)

// inDependencyOrder sorts files so each appears after every file it references.
//
// Descriptors must be built in this order because a cross-file reference needs
// the referenced file's finished descriptor. Protobuf forbids import cycles, so
// a cycle here is a malformed input rather than a case to work around.
//
// Requires: index maps every referenced declaration to a path present in files.
// Ensures: the result is a permutation of files, deterministic for a given
// input regardless of the caller's ordering.
func inDependencyOrder(files []protoir.File, index map[string]string) ([]protoir.File, error) {
	byPath := make(map[string]protoir.File, len(files))
	paths := make([]string, 0, len(files))
	for _, f := range files {
		byPath[f.Path] = f
		paths = append(paths, f.Path)
	}
	sort.Strings(paths)

	// remaining[path] is the set of not-yet-emitted files that path waits on.
	remaining := make(map[string]map[string]struct{}, len(files))
	for i := range files {
		remaining[files[i].Path] = fileDependencies(&files[i], index)
	}

	ordered := make([]protoir.File, 0, len(files))
	for len(ordered) < len(files) {
		ready := readyPaths(paths, remaining)
		if len(ready) == 0 {
			return nil, protoir.Errorf(protoir.EINVALID,
				"import cycle between %s", strings.Join(pending(paths, remaining), ", "))
		}
		for _, path := range ready {
			ordered = append(ordered, byPath[path])
			delete(remaining, path)
		}
		for _, deps := range remaining {
			for _, path := range ready {
				delete(deps, path)
			}
		}
	}
	return ordered, nil
}

// fileDependencies returns the paths of other files that file references.
func fileDependencies(file *protoir.File, index map[string]string) map[string]struct{} {
	deps := make(map[string]struct{})
	for _, m := range file.Messages {
		for _, f := range m.Fields {
			if f.Type.Kind == protoir.KindScalar {
				continue
			}
			if path, ok := index[f.Type.Ref]; ok && path != file.Path {
				deps[path] = struct{}{}
			}
		}
	}
	return deps
}

// readyPaths returns every still-pending path with no unmet dependency, in
// sorted order so the emitted sequence does not depend on map iteration.
func readyPaths(paths []string, remaining map[string]map[string]struct{}) []string {
	ready := make([]string, 0, len(remaining))
	for _, path := range paths {
		if deps, pending := remaining[path]; pending && len(deps) == 0 {
			ready = append(ready, path)
		}
	}
	return ready
}

// pending returns the paths still waiting, for a cycle report.
func pending(paths []string, remaining map[string]map[string]struct{}) []string {
	stuck := make([]string, 0, len(remaining))
	for _, path := range paths {
		if _, ok := remaining[path]; ok {
			stuck = append(stuck, path)
		}
	}
	return stuck
}
