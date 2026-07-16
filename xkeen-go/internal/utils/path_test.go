package utils

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// --- NewPathValidator tests ---

func TestNewPathValidator_ValidRoots_Succeeds(t *testing.T) {
	tmp := t.TempDir()
	pv, err := NewPathValidator([]string{tmp})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pv == nil {
		t.Fatal("expected non-nil PathValidator")
	}
}

func TestNewPathValidator_EmptyRoots_ReturnsError(t *testing.T) {
	_, err := NewPathValidator([]string{})
	if err == nil {
		t.Fatal("expected error for empty roots")
	}
	if !strings.Contains(err.Error(), "at least one") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestNewPathValidator_NilRoots_ReturnsError(t *testing.T) {
	_, err := NewPathValidator(nil)
	if err == nil {
		t.Fatal("expected error for nil roots")
	}
}

func TestNewPathValidator_MultipleRoots(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	pv, err := NewPathValidator([]string{dir1, dir2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	roots := pv.AllowedRoots()
	if len(roots) != 2 {
		t.Errorf("expected 2 roots, got %d", len(roots))
	}
}

func TestNewPathValidator_RelativeRoot_ResolvedToAbsolute(t *testing.T) {
	pv, err := NewPathValidator([]string{"."})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	roots := pv.AllowedRoots()
	for _, r := range roots {
		if !filepath.IsAbs(r) {
			t.Errorf("expected absolute root, got %q", r)
		}
	}
}

// --- Validate tests ---

func TestValidate_PathWithinRoot_ReturnsCleanedAbsPath(t *testing.T) {
	tmp := t.TempDir()
	pv, err := NewPathValidator([]string{tmp})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Create a file to validate
	filePath := filepath.Join(tmp, "test.json")
	if err := os.WriteFile(filePath, []byte("{}"), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	result, err := pv.Validate(filePath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected, _ := filepath.Abs(filePath)
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestValidate_PathOutsideRoot_ReturnsErrOutsideRoot(t *testing.T) {
	tmp := t.TempDir()
	other := t.TempDir()
	pv, err := NewPathValidator([]string{tmp})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	outsidePath := filepath.Join(other, "outside.json")
	_, err = pv.Validate(outsidePath)
	if err != ErrPathOutsideRoot {
		t.Errorf("expected ErrPathOutsideRoot, got %v", err)
	}
}

func TestValidate_PathWithDotDot_ReturnsErrTraversal(t *testing.T) {
	tmp := t.TempDir()
	pv, err := NewPathValidator([]string{tmp})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Use a raw path string with .. that filepath.Clean won't fully resolve
	// to a simple path. On Windows, filepath.Join resolves .. early, so
	// we use a literal path that still contains .. after cleaning.
	traversalPath := tmp + string(filepath.Separator) + ".." + string(filepath.Separator) + "etc" + string(filepath.Separator) + "passwd"

	_, err = pv.Validate(traversalPath)
	// Depending on platform and how filepath.Clean resolves, we get either
	// ErrPathTraversal (if .. survives in cleaned path) or ErrPathOutsideRoot
	// (if .. is resolved but the resulting path is outside root).
	// Both are acceptable rejections.
	if err != ErrPathTraversal && err != ErrPathOutsideRoot {
		t.Errorf("expected ErrPathTraversal or ErrPathOutsideRoot, got %v", err)
	}
}

func TestValidate_EmptyPath_ReturnsErrEmpty(t *testing.T) {
	tmp := t.TempDir()
	pv, err := NewPathValidator([]string{tmp})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = pv.Validate("")
	if err != ErrEmptyPath {
		t.Errorf("expected ErrEmptyPath, got %v", err)
	}
}

func TestValidate_SubdirectoryWithinRoot(t *testing.T) {
	tmp := t.TempDir()
	subdir := filepath.Join(tmp, "subdir")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}

	pv, err := NewPathValidator([]string{tmp})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := pv.Validate(subdir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected, _ := filepath.Abs(subdir)
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestValidate_NonExistentFileWithinExistingRoot(t *testing.T) {
	tmp := t.TempDir()
	pv, err := NewPathValidator([]string{tmp})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	nonExistent := filepath.Join(tmp, "does-not-exist.json")
	// Validate should succeed for non-existent paths within root
	// because the parent (tmp) exists
	_, err = pv.Validate(nonExistent)
	if err != nil {
		t.Errorf("expected valid for non-existent file within root, got: %v", err)
	}
}

func TestValidate_NonExistentFileInNonExistentSubdir(t *testing.T) {
	tmp := t.TempDir()
	pv, err := NewPathValidator([]string{tmp})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Two levels of non-existent directories, but parent (tmp) exists
	nonExistent := filepath.Join(tmp, "new-dir", "new-file.json")
	_, err = pv.Validate(nonExistent)
	if err != nil {
		t.Errorf("expected valid for nested non-existent path within root, got: %v", err)
	}
}

func TestValidate_RootPathItself(t *testing.T) {
	tmp := t.TempDir()
	pv, err := NewPathValidator([]string{tmp})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := pv.Validate(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected, _ := filepath.Abs(tmp)
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

// --- Symlink tests (platform-dependent) ---

func TestValidate_SymlinkRejectedWhenDisallowed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks behave differently on Windows")
	}

	tmp := t.TempDir()
	realDir := filepath.Join(tmp, "real")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatalf("failed to create real dir: %v", err)
	}

	linkDir := filepath.Join(tmp, "link")
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	// Validate with root = tmp, symlinks disallowed (default)
	pv, err := NewPathValidator([]string{tmp})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	fileInLink := filepath.Join(linkDir, "file.txt")
	_, err = pv.Validate(fileInLink)
	if err != ErrSymlinkDetected {
		t.Errorf("expected ErrSymlinkDetected, got %v", err)
	}
}

func TestValidate_SymlinkAllowedWhenOptionSet(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks behave differently on Windows")
	}

	tmp := t.TempDir()
	realDir := filepath.Join(tmp, "real")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatalf("failed to create real dir: %v", err)
	}

	linkDir := filepath.Join(tmp, "link")
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	// Create file inside the real dir
	realFile := filepath.Join(realDir, "file.txt")
	if err := os.WriteFile(realFile, []byte("hello"), 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	// Validate with root = tmp, symlinks allowed
	pv, err := NewPathValidator([]string{tmp}, WithSymlinks(true))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	fileInLink := filepath.Join(linkDir, "file.txt")
	_, err = pv.Validate(fileInLink)
	if err != nil {
		t.Errorf("expected symlink to be allowed, got: %v", err)
	}
}

// --- IsAllowed tests ---

func TestIsAllowed_ValidPath_ReturnsTrue(t *testing.T) {
	tmp := t.TempDir()
	pv, err := NewPathValidator([]string{tmp})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	filePath := filepath.Join(tmp, "test.json")
	if err := os.WriteFile(filePath, []byte("{}"), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	if !pv.IsAllowed(filePath) {
		t.Error("expected path to be allowed")
	}
}

func TestIsAllowed_InvalidPath_ReturnsFalse(t *testing.T) {
	tmp := t.TempDir()
	other := t.TempDir()
	pv, err := NewPathValidator([]string{tmp})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	outsidePath := filepath.Join(other, "outside.json")
	if pv.IsAllowed(outsidePath) {
		t.Error("expected path outside root to not be allowed")
	}
}

func TestIsAllowed_EmptyPath_ReturnsFalse(t *testing.T) {
	tmp := t.TempDir()
	pv, err := NewPathValidator([]string{tmp})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pv.IsAllowed("") {
		t.Error("expected empty path to not be allowed")
	}
}

// --- AllowedRoots tests ---

func TestAllowedRoots_ReturnsCopy(t *testing.T) {
	tmp := t.TempDir()
	pv, err := NewPathValidator([]string{tmp})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	roots := pv.AllowedRoots()
	// Modify the returned slice
	roots[0] = "/tampered"

	// Verify original is unchanged
	originalRoots := pv.AllowedRoots()
	if originalRoots[0] == "/tampered" {
		t.Error("AllowedRoots should return a copy, not a reference")
	}
}

// --- ValidateFilename tests ---

func TestValidateFilename_NormalNames(t *testing.T) {
	names := []string{
		"config.json",
		"03_inbounds.json",
		"my-file.yaml",
		"file with spaces.jsonc",
		"UPPER.CASE",
		"123",
		"a",
	}
	for _, name := range names {
		if err := ValidateFilename(name); err != nil {
			t.Errorf("ValidateFilename(%q) should succeed, got: %v", name, err)
		}
	}
}

func TestValidateFilename_Empty(t *testing.T) {
	if err := ValidateFilename(""); err != ErrEmptyPath {
		t.Errorf("expected ErrEmptyPath, got %v", err)
	}
}

func TestValidateFilename_ForwardSlash(t *testing.T) {
	if err := ValidateFilename("dir/file"); err != ErrPathTraversal {
		t.Errorf("expected ErrPathTraversal for forward slash, got %v", err)
	}
}

func TestValidateFilename_Backslash(t *testing.T) {
	if err := ValidateFilename("dir\\file"); err != ErrPathTraversal {
		t.Errorf("expected ErrPathTraversal for backslash, got %v", err)
	}
}

func TestValidateFilename_Dot(t *testing.T) {
	if err := ValidateFilename("."); err != ErrPathTraversal {
		t.Errorf("expected ErrPathTraversal for '.', got %v", err)
	}
}

func TestValidateFilename_DotDot(t *testing.T) {
	if err := ValidateFilename(".."); err != ErrPathTraversal {
		t.Errorf("expected ErrPathTraversal for '..', got %v", err)
	}
}

func TestValidateFilename_ContainsDotDot(t *testing.T) {
	if err := ValidateFilename("a..b"); err != ErrPathTraversal {
		t.Errorf("expected ErrPathTraversal for 'a..b', got %v", err)
	}
}

func TestValidateFilename_NullByte(t *testing.T) {
	if err := ValidateFilename("file\x00name"); err != ErrInvalidPath {
		t.Errorf("expected ErrInvalidPath for null byte, got %v", err)
	}
}

func TestValidateFilename_HiddenFile(t *testing.T) {
	// Hidden files (starting with dot) are valid filenames
	if err := ValidateFilename(".hidden"); err != nil {
		t.Errorf("expected .hidden to be valid, got: %v", err)
	}
}

func TestValidateFilename_ExtensionOnly(t *testing.T) {
	// ".json" is a valid filename (extension only)
	if err := ValidateFilename(".json"); err != nil {
		t.Errorf("expected .json to be valid, got: %v", err)
	}
}

// --- CleanPath tests ---

func TestCleanPath_CleansDotDot(t *testing.T) {
	result := CleanPath("a/b/../c")
	expected := filepath.Join("a", "c")
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestCleanPath_CleansDot(t *testing.T) {
	result := CleanPath("a/./b/./c")
	expected := filepath.Join("a", "b", "c")
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestCleanPath_AlreadyClean(t *testing.T) {
	input := filepath.Join("a", "b", "c")
	result := CleanPath(input)
	if result != input {
		t.Errorf("expected %q, got %q", input, result)
	}
}

// --- JoinPath tests ---

func TestJoinPath_JoinsElements(t *testing.T) {
	result := JoinPath("a", "b", "c")
	expected := filepath.Join("a", "b", "c")
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestJoinPath_SingleElement(t *testing.T) {
	result := JoinPath("a")
	if result != "a" {
		t.Errorf("expected %q, got %q", "a", result)
	}
}

func TestJoinPath_Empty(t *testing.T) {
	result := JoinPath()
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

// --- Error type tests ---

func TestErrorTypes_AreDistinct(t *testing.T) {
	errors := []error{ErrPathTraversal, ErrPathOutsideRoot, ErrSymlinkDetected, ErrEmptyPath, ErrInvalidPath}
	for i := 0; i < len(errors); i++ {
		for j := i + 1; j < len(errors); j++ {
			if errors[i] == errors[j] {
				t.Errorf("error %d and %d should be distinct", i, j)
			}
		}
	}
}

// --- GetBinaryNameForArch ---

func TestGetBinaryNameForArch(t *testing.T) {
	result := GetBinaryNameForArch()
	// On any architecture it should return a non-empty binary name
	if result == "" {
		t.Error("GetBinaryNameForArch should return non-empty string")
	}
	if !strings.HasPrefix(result, "xkeen-ui-") {
		t.Errorf("expected prefix 'xkeen-ui-', got %q", result)
	}
}

func TestGetBinaryNameForArch_KnownArchs(t *testing.T) {
	// Test the mapping logic by checking current arch produces expected result
	result := GetBinaryNameForArch()
	expected := map[string]string{
		"arm64":  "xkeen-ui-keenetic-arm64",
		"amd64":  "xkeen-ui-keenetic-arm64",
		"mipsle": "xkeen-ui-keenetic-mipsle",
		"mips":   "xkeen-ui-keenetic-mipsle",
	}
	if exp, ok := expected[runtime.GOARCH]; ok {
		if result != exp {
			t.Errorf("on %s: expected %q, got %q", runtime.GOARCH, exp, result)
		}
	}
	// For unknown architectures, it defaults to arm64
}

// --- WithSymlinks ---

func TestWithSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks not reliable on Windows")
	}

	tmpDir := t.TempDir()
	realDir := filepath.Join(tmpDir, "real")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create validator with symlinks allowed
	pv, err := NewPathValidator([]string{tmpDir}, WithSymlinks(true))
	if err != nil {
		t.Fatalf("NewPathValidator with WithSymlinks(true) failed: %v", err)
	}

	// Create a symlink inside allowed root
	linkPath := filepath.Join(tmpDir, "link")
	if err := os.Symlink(realDir, linkPath); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	targetFile := filepath.Join(linkPath, "test.txt")
	if err := os.WriteFile(targetFile, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Should resolve symlink and allow access
	_, err = pv.Validate(linkPath)
	if err != nil {
		t.Errorf("Validate with symlink allowed should succeed: %v", err)
	}
}

func TestWithSymlinks_Disallowed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks not reliable on Windows")
	}

	tmpDir := t.TempDir()
	realDir := filepath.Join(tmpDir, "real")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create validator with symlinks NOT allowed (default)
	pv, err := NewPathValidator([]string{tmpDir})
	if err != nil {
		t.Fatalf("NewPathValidator failed: %v", err)
	}

	// Create a symlink
	linkPath := filepath.Join(tmpDir, "link")
	if err := os.Symlink(realDir, linkPath); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	targetFile := filepath.Join(linkPath, "test.txt")
	if err := os.WriteFile(targetFile, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Should detect symlink and reject
	_, err = pv.Validate(targetFile)
	if err == nil {
		t.Error("expected error for symlink when symlinks not allowed")
	}
}

// --- validateNonExistentPath ---

func TestValidateNonExistentPath_WithinRoot(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "sub")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}

	pv, err := NewPathValidator([]string{tmpDir})
	if err != nil {
		t.Fatal(err)
	}

	// Non-existent file within existing parent
	nonExistent := filepath.Join(subDir, "newfile.json")
	result, err := pv.Validate(nonExistent)
	if err != nil {
		t.Errorf("Validate should succeed for non-existent file within root: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty resolved path")
	}
}

func TestValidateNonExistentPath_DeepNesting(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "level1")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}

	pv, err := NewPathValidator([]string{tmpDir})
	if err != nil {
		t.Fatal(err)
	}

	// Non-existent deep path — only level1 exists
	deepPath := filepath.Join(subDir, "level2", "level3", "file.json")
	result, err := pv.Validate(deepPath)
	if err != nil {
		t.Errorf("Validate should succeed for deeply nested non-existent path: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty resolved path")
	}
}

func TestValidateNonExistentPath_OutsideRoot(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "sub")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}

	otherDir := t.TempDir()

	pv, err := NewPathValidator([]string{tmpDir})
	if err != nil {
		t.Fatal(err)
	}

	// Non-existent file under a completely different root
	nonExistent := filepath.Join(otherDir, "newfile.json")
	_, err = pv.Validate(nonExistent)
	if err == nil {
		t.Error("expected error for non-existent file outside root")
	}
}
