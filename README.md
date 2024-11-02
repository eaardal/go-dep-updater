# Go Dep Updater

Bump a dependency's version in many Go projects automatically.

Given a starting directory, walk sub-directories (projects) and if it is a Go project that uses a given dependency, try to bump its version to the given version, then git commit and push.
