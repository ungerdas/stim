package transfer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ungerdas/stim/internal/client"
	"github.com/ungerdas/stim/internal/remotefiles"
	"github.com/ungerdas/stim/internal/sshtest"
	"github.com/ungerdas/stim/internal/store"
	"golang.org/x/crypto/ssh"
)

// across sets up a source with a.mkv in dir/A and a target whose server
// is reached over SSH; both "servers" are the local disk behind a test SSH
// server.
func across(t *testing.T) (e *Engine, clients fakeClients, src, dst *fake, hash, dir string) {
	t.Helper()
	host, port, key := sshtest.Serve(t, nil)
	sshInst := store.Instance{
		SSHHost: host, SSHPort: port, SSHUser: "u", SSHPassword: sshtest.Password,
		SSHHostKey: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))),
	}
	dir = t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "A"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "A", "a.mkv"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	hash = testHash(t)
	src, dst = newFake(), newFake()
	src.inst, dst.inst = sshInst, sshInst
	src.inst.SSHHostKey = "" // the source may be met for the first time
	dst.onDisk = true
	src.torrents[hash] = client.Torrent{Hash: hash, Name: "a.mkv", State: client.StateSeeding, Progress: 1, SavePath: filepath.Join(dir, "A"), Label: "tv"}
	clients = fakeClients{"src": src, "dst": dst}
	return New(clients, noKeys{}, fast), clients, src, dst, hash, dir
}

func copyRequest(hash, targetPath string, deleteSource bool) Request {
	r := request(hash, true, "")
	r.CopyFiles, r.TargetPath, r.DeleteSourceFiles = true, targetPath, deleteSource
	return r
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func TestCopyAcrossServers(t *testing.T) {
	e, _, src, dst, hash, dir := across(t)
	target := filepath.Join(dir, "B", "new")
	job, err := e.Start(copyRequest(hash, target, true))
	if err != nil {
		t.Fatal(err)
	}
	it := wait(t, e, job.ID)
	if it.State != StateDone {
		t.Fatalf("state %s %s: %s", it.State, it.Code, it.Error)
	}
	if b, _ := os.ReadFile(filepath.Join(target, "a.mkv")); string(b) != "hello" {
		t.Errorf("target file = %q", b)
	}
	if exists(filepath.Join(target, "a.mkv"+partSuffix)) {
		t.Error("partial file left behind")
	}
	got, _, _ := dst.Torrent(context.Background(), hash)
	if got.SavePath != target || got.State != client.StateSeeding {
		t.Errorf("target torrent = %+v", got)
	}
	if src.has(hash) || len(src.removedData) != 1 || exists(filepath.Join(dir, "A", "a.mkv")) {
		t.Error("source torrent and its files should be gone")
	}
	if it.CopyTotal != 5 || it.CopyDone != 5 || it.Note != "" {
		t.Errorf("item = %+v", it)
	}
}

func TestCopyKeepsSourceFilesByDefault(t *testing.T) {
	e, _, src, _, hash, dir := across(t)
	job, _ := e.Start(copyRequest(hash, filepath.Join(dir, "B"), false))
	if it := wait(t, e, job.ID); it.State != StateDone {
		t.Fatalf("got %+v", it)
	}
	if src.has(hash) || len(src.removedData) != 0 || !exists(filepath.Join(dir, "A", "a.mkv")) {
		t.Error("source torrent should be removed, its files kept")
	}
}

func TestCopyFailedCheckRemovesCopies(t *testing.T) {
	e, _, src, dst, hash, dir := across(t)
	dst.onDisk, dst.dataFound = false, 0.5
	job, _ := e.Start(copyRequest(hash, filepath.Join(dir, "B", "new"), true))
	it := wait(t, e, job.ID)
	if it.State != StateFailed || it.Code != CodeDataNotFound {
		t.Fatalf("got %+v", it)
	}
	if exists(filepath.Join(dir, "B")) {
		t.Error("the copy and the folders it made should be removed")
	}
	if !src.has(hash) || !exists(filepath.Join(dir, "A", "a.mkv")) || dst.has(hash) {
		t.Error("source must be untouched and the target rolled back")
	}
}

func TestCopyKeepsExistingTargetFolders(t *testing.T) {
	e, _, _, dst, hash, dir := across(t)
	dst.onDisk, dst.dataFound = false, 0.5
	if err := os.MkdirAll(filepath.Join(dir, "B"), 0o755); err != nil {
		t.Fatal(err)
	}
	job, _ := e.Start(copyRequest(hash, filepath.Join(dir, "B"), false))
	wait(t, e, job.ID)
	if !exists(filepath.Join(dir, "B")) || exists(filepath.Join(dir, "B", "a.mkv")) {
		t.Error("only what the copy created should be removed")
	}
}

func TestCopyMissingSourceFile(t *testing.T) {
	e, _, src, _, hash, dir := across(t)
	_ = os.Remove(filepath.Join(dir, "A", "a.mkv"))
	job, _ := e.Start(copyRequest(hash, filepath.Join(dir, "B"), true))
	it := wait(t, e, job.ID)
	if it.State != StateFailed || it.Code != CodeSourceFileMissing {
		t.Fatalf("got %+v", it)
	}
	if !src.has(hash) || exists(filepath.Join(dir, "B")) {
		t.Error("nothing should change")
	}
}

func TestCopyKeepsFilesOtherTorrentsUse(t *testing.T) {
	e, clients, src, _, hash, dir := across(t)
	other := newFake()
	other.inst = src.inst // another client on the source server
	other.torrents["x"] = client.Torrent{Hash: "x", Name: "a.mkv", SavePath: filepath.Join(dir, "A")}
	clients["other"] = other
	job, _ := e.Start(copyRequest(hash, filepath.Join(dir, "B"), true))
	it := wait(t, e, job.ID)
	if it.State != StateDone || it.Note != NoteShared || it.Shared != 1 {
		t.Fatalf("got %+v", it)
	}
	if src.has(hash) || len(src.removedData) != 0 || !exists(filepath.Join(dir, "A", "a.mkv")) {
		t.Error("the torrent should leave the source but its files stay")
	}
}

func TestCopyToTheSameFilesKeepsThem(t *testing.T) {
	e, _, src, _, hash, dir := across(t)
	job, _ := e.Start(copyRequest(hash, filepath.Join(dir, "A"), true))
	it := wait(t, e, job.ID)
	if it.State != StateDone || it.Note != NoteSameFiles {
		t.Fatalf("got %+v", it)
	}
	if len(src.removedData) != 0 || !exists(filepath.Join(dir, "A", "a.mkv")) {
		t.Error("files the target now uses must not be deleted")
	}
}

func TestCopyResumesAndCountsExistingFiles(t *testing.T) {
	e, _, _, _, hash, dir := across(t)
	target := filepath.Join(dir, "B")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "a.mkv"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	job, _ := e.Start(copyRequest(hash, target, false))
	it := wait(t, e, job.ID)
	if it.State != StateDone || it.CopyDone != 5 {
		t.Fatalf("got %+v", it)
	}
}

func TestCopyValidation(t *testing.T) {
	e, _, _, dst, hash, dir := across(t)
	if _, err := e.Start(copyRequest(hash, "relative/path", false)); err == nil {
		t.Error("relative target path accepted")
	}
	r := request(hash, false, "")
	r.DeleteSourceFiles = true
	if _, err := e.Start(r); err == nil {
		t.Error("deleting source files without a copy accepted")
	}
	r = copyRequest(hash, filepath.Join(dir, "B"), true)
	r.RemoveFromSource = false
	if _, err := e.Start(r); err == nil {
		t.Error("deleting source files while keeping the source torrent accepted")
	}
	dst.inst.SSHHostKey = ""
	if _, err := e.Start(copyRequest(hash, filepath.Join(dir, "B"), false)); err == nil {
		t.Error("a target server with no stored host key accepted")
	}
	dst.inst = store.Instance{}
	if _, err := e.Start(copyRequest(hash, filepath.Join(dir, "B"), false)); err == nil {
		t.Error("a target without SSH accepted")
	}
}

func TestCopyNoSpace(t *testing.T) {
	// The real check needs statvfs; just make sure the helper reads it.
	host, port, key := sshtest.Serve(t, nil)
	s, err := remotefiles.Open(context.Background(), remotefiles.Config{Host: host, Port: port, User: "u", Password: sshtest.Password,
		HostKey: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))}, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if free := s.Free(filepath.Join(t.TempDir(), "not", "there")); free <= 0 {
		t.Errorf("free = %d", free)
	}
}

func TestCopyNeverOverwritesOtherFiles(t *testing.T) {
	e, _, src, _, hash, dir := across(t)
	target := filepath.Join(dir, "B")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "a.mkv"), []byte("another release"), 0o644); err != nil {
		t.Fatal(err)
	}
	job, _ := e.Start(copyRequest(hash, target, true))
	it := wait(t, e, job.ID)
	if it.State != StateFailed || it.Code != CodeTargetExists {
		t.Fatalf("got %+v", it)
	}
	if b, _ := os.ReadFile(filepath.Join(target, "a.mkv")); string(b) != "another release" {
		t.Errorf("the existing file was changed: %q", b)
	}
	if !src.has(hash) {
		t.Error("the source should be untouched")
	}
}
