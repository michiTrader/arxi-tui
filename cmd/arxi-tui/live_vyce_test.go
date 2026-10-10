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

// Runs only with VYCE_API_KEY set (never committed): a real model, the real core.
func TestLiveModelUsesTheInterfaceTools(t *testing.T) {
	key := os.Getenv("VYCE_API_KEY")
	if key == "" {
		t.Skip("VYCE_API_KEY not set")
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
	if out := runHubWork(ctx, sd.Hub(), hubWork{Op: opDefault, Ref: "vyce/deepseek-v4-flash", Close: true}); !out.ok {
		t.Fatalf("default: %q", out.notice)
	}
	sd.SetEffort("high")
	br := sd.InterfaceBridge()
	doc := builtinDoc(t)
	br.publish(doc, theme.SOBRIA(), nil, nil, behaviour{})
	if err := sd.SubmitPrompt(ctx, "pon una linea de colores animada alrededor de la barra de entrada de la tui"); err != nil {
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
			// The real loop republishes what is on screen after every applied change
			// (main.go uiBr.publish); without it the next call would not see the
			// animation the previous one defined.
			doc = req.doc
			beh := req.baseBeh
			if req.behChanged {
				beh = req.beh
			}
			user, words := req.baseColors, req.baseTexts
			if req.colors != nil {
				user = req.colors
			}
			if req.texts != nil {
				words = req.texts
			}
			br.publish(doc, theme.SOBRIA(), user, words, beh)
			req.done <- nil
		case <-ctx.Done():
			t.Fatalf("timeout; tools=%v", tools)
		}
	}
	t.Logf("tools: %s", strings.Join(tools, " | "))
	t.Logf("answer: %.400s", answer)
	f := findNodeByID(doc, "input_frame")
	if f == nil || f.BorderStyleName() == "" {
		t.Fatalf("the model said it framed the input but the document has no styled frame (answer: %.200s)", answer)
	}
	t.Logf("frame: shape=%s style=%s", f.BorderShape(), f.BorderStyleName())
}
