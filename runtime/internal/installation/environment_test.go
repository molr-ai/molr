//go:build linux || darwin

package installation

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"molr.ai/runtime/internal/execenv"
)

func TestInstallationEnvironmentPlanPreservesUpgrade(t *testing.T) {
	e, r := fixture(t)
	e.Options.Port = 18787
	e.environment = []string{"PATH=" + t.TempDir(), "CUSTOM=do-not-import"}
	p, err := e.plan(&Package{Build: r.Build}, nil)
	if err != nil || len(p.Environment) != 1 || p.Environment["PATH"] == "" {
		t.Fatal(p, err)
	}
	p, err = e.plan(&Package{Build: r.Build}, r)
	if err != nil || p.Environment != nil {
		t.Fatal("upgrade captured new environment", err)
	}
	e.Options.Environment = "CUSTOM"
	if _, err := e.plan(&Package{Build: r.Build}, r); err == nil {
		t.Fatal("upgrade accepted implicit environment change")
	}
}

func TestInstallationEnvironmentCaptureAndRefresh(t *testing.T) {
	e, r := fixture(t)
	path := filepath.Join(t.TempDir(), "tools")
	e.environment = []string{"PATH=" + path, "JAVA_HOME=selected-secret", "UNSELECTED=private"}
	// The installer has already restricted its own PATH by this point.
	t.Setenv("PATH", "/usr/bin:/bin")
	if err := e.writeEnvironment(r, nil); err != nil {
		t.Fatal(err)
	}
	got, err := execenv.Read(r.DataDir)
	if err != nil || len(got) != 1 || got["PATH"] != path {
		t.Fatal(got, err)
	}
	info, err := os.Stat(filepath.Join(r.DataDir, execenv.Filename))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal(info, err)
	}
	e.Options.Environment = "JAVA_HOME"
	if err = e.writeEnvironment(r, nil); err != nil {
		t.Fatal(err)
	}
	e.environment[0] = "PATH=" + filepath.Join(path, "next")
	e.Options.Environment = ""
	if err = e.writeEnvironment(r, nil); err != nil {
		t.Fatal(err)
	}
	got, err = execenv.Read(r.DataDir)
	if err != nil || len(got) != 2 || got["JAVA_HOME"] != "selected-secret" || got["PATH"] != filepath.Join(path, "next") {
		t.Fatal(got, err)
	}
	before, _ := os.ReadFile(filepath.Join(r.DataDir, execenv.Filename))
	e.Options.Environment = "PATH,MISSING"
	if err = e.writeEnvironment(r, nil); err == nil {
		t.Fatal("missing variable accepted")
	}
	after, _ := os.ReadFile(filepath.Join(r.DataDir, execenv.Filename))
	if !bytes.Equal(before, after) {
		t.Fatal("failed selection partially saved")
	}
	if os.Getenv("PATH") != "/usr/bin:/bin" {
		t.Fatal("installer environment mutated")
	}
}

func TestInstallerDoesNotExposeEnvironmentCommand(t *testing.T) {
	for _, args := range [][]string{{"environment"}, {"update", "--env", "PATH"}, {"status", "--env", "PATH"}} {
		if _, err := parseOptions(args, io.Discard); err == nil {
			t.Fatal("accepted unsupported environment command", args)
		}
	}
}
