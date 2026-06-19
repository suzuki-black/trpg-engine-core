package engine

import (
	"math/rand"
	"testing"

	"trpg-engine-core/internal/gm"
	"trpg-engine-core/internal/llm"
	"trpg-engine-core/internal/npc"
	"trpg-engine-core/internal/scenario"
	"trpg-engine-core/internal/state"
)

func TestZVisible(t *testing.T) {
	scn := scenario.ForgottenShrine()
	sess := state.NewSession()
	sess.ChapterID = "ch01"
	sess.SceneSummary = scn.Chapter("ch01").SceneSummary
	mock := llm.NewMock()
	eng := New(scn, sess, rand.New(rand.NewSource(1)), gm.New(mock, 0), npc.New(mock, 0))
	ch := scn.Chapter("ch01")
	t.Logf("present chars: %v", eng.presentCharacters(ch))
	eng.revealEntities(ch, "情報屋カラスに声をかけます", "talk")
	t.Logf("known after greeting: %v", sess.KnownEntities)
	var names []string
	for _, e := range eng.visibleEntities(ch) {
		names = append(names, e.Name)
	}
	t.Logf("visible entities: %v", names)
}
