package ingest

// WrapperArgv exposes the limit wrapper's argv so a test can run the real shell
// script with a value that makes ulimit fail.
func WrapperArgv(mem, cpu string, argv []string) []string {
	return append([]string{"sh", "-c", wrapperScript, "sh", mem, cpu}, argv...)
}
