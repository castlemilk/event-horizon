package lmac

import "testing"

func TestTaskIDs(t *testing.T) {
	if TaskMM != 0 || TaskDBG != 1 || TaskSCANU != 4 {
		t.Fatalf("task id drift: MM=%d DBG=%d SCANU=%d", TaskMM, TaskDBG, TaskSCANU)
	}
}

func TestLMACFirstMsg(t *testing.T) {
	if FirstMsg(TaskMM) != 0x0000 || FirstMsg(TaskDBG) != 0x0400 || FirstMsg(TaskSCANU) != 0x1000 {
		t.Fatalf("FirstMsg arithmetic drifted")
	}
}

func TestMessageIDConstants(t *testing.T) {
	cases := []struct{ got, want uint16 }{
		{MMVersionReq, 0x0004},
		{MMVersionCfm, 0x0005},
		{SCANUStartReq, 0x1000},
		{SCANUResultInd, 0x1004},
		{MMDbgTlvCmdReq, 0x0482},
		{DBGMemReadReq, 0x0400},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("msg id = 0x%04x, want 0x%04x", c.got, c.want)
		}
	}
}
