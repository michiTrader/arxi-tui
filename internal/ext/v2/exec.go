package extv2

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type execSeams struct {
	// environ returns the host environment; nil means os.Environ. A test fills it
	// to prove what does and does not reach a child.
	environ func() []string
}

// maxArgElem bounds one argument after its placeholders are filled. The system has its
// own limit and a failure there would read as a mystery.
const maxArgElem = 16 * 1024

// envAllow is every variable a child may inherit. The list is short on purpose: the
// host's environment holds provider keys and tokens, and a script a package ships (or
// a program it names) must not be able to read them. These are the ones programs need
// to find themselves, a home, a temp folder and a language.
var envAllow = map[string]bool{
	"PATH": true, "HOME": true, "USERPROFILE": true, "LANG": true, "LC_ALL": true,
	"TMPDIR": true, "TEMP": true, "TMP": true, "TERM": true, "TZ": true,
	// Windows cannot start most programs without these.
	"SYSTEMROOT": true, "SYSTEMDRIVE": true, "COMSPEC": true, "PATHEXT": true,
	"WINDIR": true, "USERNAME": true, "LOCALAPPDATA": true, "APPDATA": true,
}

func (s execSeams) childEnv(pkgDir string) []string {
	env := os.Environ
	if s.environ != nil {
		env = s.environ
	}
	var out []string
	for _, kv := range env() {
		k, _, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if envAllow[strings.ToUpper(k)] {
			out = append(out, kv)
		}
	}
	if pkgDir != "" {
		out = append(out, "ARXI_PACKAGE_DIR="+pkgDir)
	}
	return out
}

// cappedBuffer keeps the first max bytes and throws the rest away while still
// accepting every write, so a chatty child is never blocked on a full pipe and the
// host never holds more than the limit.
type cappedBuffer struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	max       int
	truncated bool
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	room := c.max - c.buf.Len()
	if room > 0 {
		if len(p) <= room {
			c.buf.Write(p)
		} else {
			c.buf.Write(p[:room])
			c.truncated = true
		}
	} else if len(p) > 0 {
		c.truncated = true
	}
	return len(p), nil
}

func (c *cappedBuffer) bytes() ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.buf.Bytes()...), c.truncated
}

// resolveProgram finds what argv[0] means. A "./name" is a file inside the package; it
// is resolved against the package's folder and must still be inside it once symbolic
// links are followed, because a link is the way a file inside a folder points outside
// it. A bare name must be listed in permissions.exec and is looked up on PATH.
func (r *Runner) resolveProgram(p *Package, prog string) (path, dir string, err error) {
	if isPackagePath(prog) {
		if r.PackageDir == nil {
			return "", "", fmt.Errorf("%s is a file of the package, but this package was not installed as a folder, so it has no files; install it from a folder, or use a program from permissions.exec", prog)
		}
		root := r.PackageDir(p.ID)
		if root == "" {
			return "", "", fmt.Errorf("%s is a file of the package, but package %q has no folder on disk; install it from a folder", prog, p.ID)
		}
		realRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			return "", "", fmt.Errorf("the folder of package %q cannot be read (%v)", p.ID, firstLine(err.Error()))
		}
		rel := filepath.FromSlash(strings.TrimPrefix(prog, "./"))
		if !filepath.IsLocal(rel) {
			return "", "", fmt.Errorf("%s resolves outside the package folder, so it is not run", prog)
		}
		full, err := filepath.EvalSymlinks(filepath.Join(realRoot, rel))
		if err != nil {
			return "", "", fmt.Errorf("%s was not found in the package folder; check the file name", prog)
		}
		inside, err := filepath.Rel(realRoot, full)
		if err != nil || !filepath.IsLocal(inside) {
			return "", "", fmt.Errorf("%s is a link that leads outside the package folder, so it is not run; the digest cannot vouch for what it points at", prog)
		}
		st, err := os.Stat(full)
		if err != nil || !st.Mode().IsRegular() {
			return "", "", fmt.Errorf("%s is not a regular file, so it cannot be run", prog)
		}
		return full, realRoot, nil
	}
	if !contains(p.Permissions.Exec, prog) {
		return "", "", fmt.Errorf("program %q is not in permissions.exec, so it was not run; add it so the user is told before it can start", prog)
	}
	full, err := exec.LookPath(prog)
	if err != nil {
		return "", "", fmt.Errorf("the program %q was not found on this machine; install it, or remove the command that needs it", prog)
	}
	if !filepath.IsAbs(full) {
		if abs, aerr := filepath.Abs(full); aerr == nil {
			full = abs
		}
	}
	return full, "", nil
}

func (r *Runner) runExec(ctx context.Context, p *Package, o Op, oc opCtx, in Input) (Outcome, error) {
	if len(o.Argv) == 0 {
		return Outcome{}, fmt.Errorf("argv is empty, so there is nothing to run")
	}
	path, dir, err := r.resolveProgram(p, o.Argv[0])
	if err != nil {
		return Outcome{}, err
	}
	argv := make([]string, 0, len(o.Argv)-1)
	for i, a := range o.Argv[1:] {
		parts, err := parseTemplate(a)
		if err != nil {
			return Outcome{}, err
		}
		v := expand(parts, in, nil)
		if len(v) > maxArgElem {
			return Outcome{}, fmt.Errorf("argument %d is %d bytes after your text is filled in, more than the %d allowed; use a shorter text", i+1, len(v), maxArgElem)
		}
		if strings.ContainsRune(v, 0) {
			return Outcome{}, fmt.Errorf("argument %d holds a NUL byte, which ends a string for the system and hides what follows; remove it", i+1)
		}
		// A value that is a whole argument and starts with '-' would be read by the
		// program as an option of its own, so what the user typed would decide how
		// the program behaves. It is refused unless the package says it expects it.
		if !o.AllowDash && len(parts) == 1 && parts[0].ph != "" && strings.HasPrefix(v, "-") {
			return Outcome{}, fmt.Errorf("your text starts with '-', and this command passes it to %s as a whole argument, where it would be read as an option; start the text with a letter, or ask the author to set allow_dash", o.Argv[0])
		}
		argv = append(argv, v)
	}

	ctx, cancel := context.WithTimeout(ctx, timeoutFor(o, oc))
	defer cancel()
	cmd := exec.CommandContext(ctx, path, argv...)
	cmd.Env = r.exec.childEnv(dir)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Stdin = nil
	out := &cappedBuffer{max: MaxOutputBytes}
	errOut := &cappedBuffer{max: 4096}
	cmd.Stdout, cmd.Stderr = out, errOut
	prepareChild(cmd)
	cmd.Cancel = func() error { return killChild(cmd) }
	cmd.WaitDelay = 500 * time.Millisecond

	if err := cmd.Start(); err != nil {
		return Outcome{}, fmt.Errorf("%s could not be started (%s); check that it is installed and may be run", filepath.Base(path), firstLine(err.Error()))
	}
	werr := cmd.Wait()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return Outcome{}, fmt.Errorf("%s did not finish in time and was stopped; try again, or raise timeout_ms (at most %d)", filepath.Base(path), MaxCommandTimeoutMS)
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return Outcome{}, fmt.Errorf("%s was cancelled", filepath.Base(path))
	}
	if werr != nil {
		eb, _ := errOut.bytes()
		why := firstLine(strings.TrimSpace(CleanText(string(eb), 200, 3)))
		if why == "" {
			why = firstLine(werr.Error())
		}
		return Outcome{}, fmt.Errorf("%s failed: %s", filepath.Base(path), why)
	}
	data, truncated := out.bytes()
	return finish(o, oc, data, truncated)
}
