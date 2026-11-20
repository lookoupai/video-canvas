package reference_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"video-canvas/internal/reference"
)

func referenceZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for name, body := range files {
		f, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestDecodeReferenceSheet_PreservesIndependentDialogues(t *testing.T) {
	data := []byte(`{"schema_version":"capsule_cinema.remake_reference.v1","duration":10,"shots":[{"shot":1,"start":0,"end":2.5,"seen":"甲推开门","camera_text":"推进"},{"shot":2,"start":2.5,"end":10,"seen":"乙回头"}],"captions":[{"start":2,"end":4,"text":"你怎么来了？","speaker":"乙"},{"start":4,"end":6,"text":"来还钥匙。","speaker":"甲"}]}`)
	doc, err := reference.DecodePackage(data, 10000)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Shots) != 2 || doc.Shots[0].EndMs != 2500 || doc.Shots[0].Action != "甲推开门" || len(doc.Dialogues) != 2 || doc.Dialogues[0].StartMs != 2000 || doc.Dialogues[0].EndMs != 4000 || len(doc.Characters) != 2 {
		t.Fatalf("导入结果 %+v", doc)
	}
	if doc.Dialogues[0].Speaker == doc.Dialogues[1].Speaker {
		t.Fatal("双人对白身份丢失")
	}
	encoded, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	again, err := reference.DecodePackage(encoded, 10000)
	if err != nil || len(again.Dialogues) != 2 {
		t.Fatalf("标准格式重导入 %+v %v", again, err)
	}
}

func TestDecodeVideoMD_UsesExactShotTimeAndVTT(t *testing.T) {
	data := referenceZip(t, map[string]string{
		"episode/VIDEO.md":             "---\nvideomd: 1\nsource: {duration: 10}\n---\n",
		"episode/video/timeline.md":    "---\nvideomd: 1\nscene: 1\n---\n\n```videomd-timeline\n#01 0.0-2.5 | seen 推开门 | camera push in\n#02 2.5-10.0 | seen 抬头回应\n```",
		"episode/video/shots/01.md":    "---\nvideomd: 1\nshot: 1\ntime: [0, 2.467]\n---\n",
		"episode/video/transcript.vtt": "WEBVTT\n\n00:00:02.000 --> 00:00:04.000\n<v 乙>你怎么来了？\n\n00:00:04.000 --> 00:00:06.000\n<v 甲>来还钥匙。",
	})
	doc, err := reference.DecodePackage(data, 10000)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Shots[0].EndMs != 2467 || len(doc.Dialogues) != 2 || doc.Dialogues[0].Text != "你怎么来了？" || len(doc.Scenes) != 1 {
		t.Fatalf("%+v", doc)
	}
}

func TestDecodeReference_RejectsInvalidPackages(t *testing.T) {
	for name, data := range map[string][]byte{
		"negative":  []byte(`{"shots":[{"shot":1,"start":-1,"end":2}]}`),
		"outside":   []byte(`{"shots":[{"shot":1,"start":0,"end":11}]}`),
		"duplicate": []byte(`{"shots":[{"shot":1,"start":0,"end":2},{"shot":1,"start":2,"end":3}]}`),
		"duration":  []byte(`{"duration":20,"shots":[{"shot":1,"start":0,"end":2}]}`),
		"path":      referenceZip(t, map[string]string{"../VIDEO.md": "bad"}),
		"multiple":  referenceZip(t, map[string]string{"one/VIDEO.md": "bad", "two/VIDEO.md": "bad"}),
		"empty":     []byte("{}"),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := reference.DecodePackage(data, 10000)
			if !errors.Is(err, reference.ErrPackage) {
				t.Fatalf("应为拆片包错误，实际 %v", err)
			}
		})
	}
}
