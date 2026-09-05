// Command cgocheck syntax-checks the darwin-only cgo and Objective-C
// bridge in internal/reopen on any host with clang. `GOOS=darwin go
// vet` needs a macOS toolchain, so Linux, Windows and even the Linux
// CI never see that file — which is how prose in the cgo preamble and
// a missing C declaration got all the way to a macOS CI run.
//
// This tool runs the platform-independent `go tool cgo` translation on
// reopen_darwin.go, then parses the generated C — and reopen_darwin.m,
// against minimal AppKit/objc stub headers — with clang -fsyntax-only.
// Run it before pushing: `make cgocheck`.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "cgocheck: "+format+"\n", args...)
	os.Exit(1)
}

func run(name string, args ...string) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		fail("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
}

// Minimal stand-ins so the bridge sources parse without Apple SDKs.
// They only need to satisfy -fsyntax-only, not link or run.
const objcStub = `// cgocheck stub: objc runtime basics
#ifndef CGOCHECK_OBJC_STUB_H
#define CGOCHECK_OBJC_STUB_H
typedef struct objc_object *id;
typedef struct objc_class *Class;
typedef struct objc_selector *SEL;
typedef id (*IMP)(id, SEL, ...);
typedef signed char BOOL;
#define YES ((BOOL)1)
#define NO  ((BOOL)0)
#define Nil ((Class)0)
#define nil ((id)0)
#endif
`

const runtimeStub = `// cgocheck stub: the runtime calls the bridge uses
#import <objc/objc.h>
Class objc_getClass(const char *name);
BOOL class_addMethod(Class cls, SEL name, IMP imp, const char *types);
`

const appkitStub = `// cgocheck stub: the AppKit surface the bridge touches
#import <objc/objc.h>
@interface NSObject
@end
@class NSApplication;
`

func main() {
	// Locate internal/reopen from the repo root (or from tools/cgocheck
	// when run by a direct `go build`).
	var dir string
	for _, candidate := range []string{"internal/reopen", filepath.Join("..", "..", "internal", "reopen")} {
		if fi, err := os.Stat(candidate); err == nil && fi.IsDir() {
			dir = candidate
			break
		}
	}
	if dir == "" {
		fail("cannot find internal/reopen; run from the repository root")
	}
	if _, err := exec.LookPath("clang"); err != nil {
		fail("clang is required (pacman -S clang)")
	}

	tmp, err := os.MkdirTemp("", "cgocheck-")
	if err != nil {
		fail("temp dir: %v", err)
	}
	defer os.RemoveAll(tmp)

	// The Go/cgo translation. Platform-independent: produces the same
	// generated C on every host, including everything cgo pasted in.
	run("go", "tool", "cgo", "-objdir", tmp, filepath.Join(dir, "reopen_darwin.go"))

	// Stub headers for the Objective-C syntax pass.
	stubs := filepath.Join(tmp, "stubs")
	for name, content := range map[string]string{
		filepath.Join("objc", "objc.h"):     objcStub,
		filepath.Join("objc", "runtime.h"):  runtimeStub,
		filepath.Join("AppKit", "AppKit.h"): appkitStub,
	} {
		p := filepath.Join(stubs, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			fail("stub dir: %v", err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			fail("write stub: %v", err)
		}
	}

	// Generated C: the cgo2 files carry the pasted preamble — exactly
	// where prose leaks and undeclared references show up.
	cFiles, err := filepath.Glob(filepath.Join(tmp, "*.cgo2.c"))
	if err != nil || len(cFiles) == 0 {
		fail("no generated C found; did `go tool cgo` run? (%v, %d files)", err, len(cFiles))
	}
	cFiles = append(cFiles, filepath.Join(tmp, "_cgo_export.c"))
	run("clang", append([]string{"-fsyntax-only", "-I", tmp}, cFiles...)...)

	// The Objective-C source, against the stub headers.
	run("clang", "-fsyntax-only", "-x", "objective-c",
		"-I", stubs, filepath.Join(dir, "reopen_darwin.m"))

	fmt.Println("cgocheck: reopen darwin bridge parses cleanly")
}
