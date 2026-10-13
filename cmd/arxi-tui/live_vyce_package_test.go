package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/theme"
)

// Runs only with VYCE_API_KEY set (never committed); VYCE_MODEL picks the model. A real
// model is asked for a command and has to write it as a package through ui_edit: the
// document must pass the host's validation, land in the user's packages folder, and do
// nothing until the user approves it.
func TestLiveModelWritesAPackage(t *testing.T) {
	key := os.Getenv("VYCE_API_KEY")
	if key == "" {
		t.Skip("VYCE_API_KEY not set")
	}
	model := os.Getenv("VYCE_MODEL")
	if model == "" {
		model = "deepseek-v4-flash"
	}
	bin := buildCore(t)
	work := t.TempDir()
	t.Setenv("ARXI_SECRETS_DIR", filepath.Join(work, "secrets"))
	t.Setenv("ARXI_PROVIDERS_DIR", filepath.Join(work, "providers"))
	t.Setenv("ARXI_BIN", bin)
	t.Setenv("HOME", work)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(work, "cfg"))
	t.Setenv(configDirEnv, filepath.Join(work, "arxi"))
	t.Chdir(work)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	drv, evCh, err := openServeDriver(ctx, bin)
	if err != nil {
		t.Fatal(err)
	}
	defer drv.Close()
	sd := drv.(*serveDriver)
	if out := runHubWork(ctx, sd.Hub(), hubWork{Op: opAdd, Name: "vyce", BaseURL: "https://vyceai.com/v1", Key: key}); !out.ok {
		t.Fatalf("add: %q", out.notice)
	}
	if out := runHubWork(ctx, sd.Hub(), hubWork{Op: opDefault, Ref: "vyce/" + model, Close: true}); !out.ok {
		t.Fatalf("default: %q", out.notice)
	}
	if !strings.Contains(model, "claude") { // that one takes no thinking level
		sd.SetEffort("high")
	}
	br := sd.InterfaceBridge()
	doc := builtinDoc(t)
	br.publish(doc, theme.SOBRIA(), nil, nil, behaviour{})
	cwd, _ := os.Getwd()
	if err := sd.SubmitPrompt(ctx, "/ui crea un comando /eco que repita lo que escribo despues, como paquete (sin programas)"); err != nil {
		t.Fatal(err)
	}
	var tools []string
	var answer string
	for answer == "" {
		select {
		case ev := <-evCh:
			switch ev.Type {
			case "chat.error":
				t.Fatalf("chat error: %v", ev.Payload["text"])
			case "chat.tool":
				s, _ := ev.Payload["summary"].(string)
				n, _ := ev.Payload["name"].(string)
				tools = append(tools, n+": "+s)
			case "chat.approval":
				sd.Decide(true)
			case "llm.response":
				answer, _ = ev.Payload["text"].(string)
			}
		case req := <-br.apply:
			if req.pkg != nil {
				if err := applyPackageEdit(req.pkg, cwd); err != nil {
					req.done <- err
					continue
				}
			}
			doc = req.doc
			br.publish(doc, theme.SOBRIA(), nil, nil, req.baseBeh)
			req.done <- nil
		case <-ctx.Done():
			t.Fatalf("timeout; tools=%v", tools)
		}
	}
	t.Logf("model: %s", model)
	t.Logf("tools: %s", strings.Join(tools, " | "))
	t.Logf("answer: %.500s", answer)
	set := loadAllPackages(cwd, behaviour{})
	t.Logf("problems: %v", set.problems)
	if len(set.pkgs) == 0 {
		t.Fatalf("no package was written (answer: %.200s)", answer)
	}
	for _, p := range set.pkgs {
		raw, _ := os.ReadFile(p.Path)
		t.Logf("package %s granted=%v runnable=%v:\n%s", p.ID, p.Granted, p.runnable(), raw)
		if p.runnable() {
			t.Errorf("a package the model wrote must not run before the user approves it")
		}
	}
}
