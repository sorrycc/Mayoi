package remote

import (
	"io"
	"os/exec"
)

// frameShell is the long-lived POSIX shell inside one Windows worker.
// ssh's standard streams are the frame connection. Each command is one
// C frame, then I frames and E. The shell runs it and answers with O, R
// and X. Nothing is installed on the host: sh, dd, mkfifo and mktemp are
// enough, and the bytes may contain NUL.
const frameShell = `
exec 7<&0 8>&1
d=$(mktemp -d "${TMPDIR:-/tmp}/mayoi-frame.XXXXXX") || exit 1
trap 'rm -rf "$d"' EXIT
mkfifo "$d/in" "$d/out" "$d/err" || exit 1
emit() {
  while ! mkdir "$d/lock" 2>/dev/null; do
    sleep 0.05
  done
  n=$(wc -c < "$2" | tr -cd 0-9)
  printf '%s %s\n' "$1" "$n" >&8
  dd if="$2" bs=32768 >&8 2>/dev/null
  rmdir "$d/lock"
}
pump() {
  # One open for the whole command. Opening again after the writer has
  # gone blocks until a new writer appears, so the exit code never follows.
  exec 3<"$1" || exit 1
  while true; do
    dd bs=32768 count=1 <&3 >"$d/c.$2" 2>/dev/null
    n=$(wc -c < "$d/c.$2" | tr -cd 0-9)
    if [ "$n" -eq 0 ]; then
      break
    fi
    emit "$2" "$d/c.$2"
  done
}
while IFS= read -r hdr <&7; do
  kind=${hdr%% *}
  n=${hdr#* }
  if [ "$kind" != C ]; then
    exit 1
  fi
  if [ "$n" -eq 0 ]; then
    : > "$d/script"
  else
    dd bs=1 count="$n" <&7 >"$d/script" 2>/dev/null || exit 1
  fi
  pump "$d/out" O &
  opid=$!
  pump "$d/err" R &
  epid=$!
  if command -v stdbuf >/dev/null 2>&1; then
    stdbuf -o0 -e0 sh "$d/script" <"$d/in" >"$d/out" 2>"$d/err" &
  else
    sh "$d/script" <"$d/in" >"$d/out" 2>"$d/err" &
  fi
  spid=$!
  exec 9>"$d/in"
  drop=0
  while IFS= read -r ih <&7; do
    ik=${ih%% *}
    il=${ih#* }
    if [ "$ik" = E ]; then
      break
    fi
    if [ "$ik" != I ]; then
      exit 1
    fi
    if [ "$il" -eq 0 ]; then
      continue
    fi
    if [ "$drop" -eq 1 ]; then
      dd bs=1 count="$il" <&7 >/dev/null 2>/dev/null
    else
      dd bs=1 count="$il" <&7 >&9 2>/dev/null || drop=1
    fi
  done
  exec 9>&-
  wait "$spid"
  code=$?
  wait "$opid"
  wait "$epid"
  printf '%s' "$code" > "$d/code"
  emit X "$d/code"
done
`

// poolSSHArgs is the ssh a Windows worker runs. The user's config and
// keys apply because the host is the one hostArgs names and no file is
// forced with -F. There is no ControlPath and no ControlMaster: Windows
// OpenSSH cannot use that socket.
func poolSSHArgs(host string) []string {
	args := []string{
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=15",
		"-o", "ServerAliveInterval=10",
		"-o", "ServerAliveCountMax=3",
		"-o", "LogLevel=ERROR",
		"-T",
	}
	args = append(args, hostArgs(host)...)
	return append(args, "sh -c "+quote(frameShell))
}

// masterCommandArgs is the ssh a macOS or Linux connection holds open.
func masterCommandArgs(sock, host string) []string {
	args := append(options(sock), "-o", "ControlMaster=yes", "-o", "ControlPersist=no", "-N")
	return append(args, hostArgs(host)...)
}

// hiddenCommand is a child whose console window stays off the desktop.
// ssh and wsl are console programs; Windows would otherwise open a
// window for each one.
func hiddenCommand(name string, arg ...string) *exec.Cmd {
	cmd := exec.Command(name, arg...)
	hideConsole(cmd)
	return cmd
}

func startSSH(host string, stderr io.Writer) (workerPipe, error) {
	cmd := hiddenCommand("ssh", poolSSHArgs(host)...)
	if stderr == nil {
		cmd.Stderr = io.Discard
	} else {
		cmd.Stderr = stderr
	}
	in, err := cmd.StdinPipe()
	if err != nil {
		return workerPipe{}, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return workerPipe{}, err
	}
	if err := cmd.Start(); err != nil {
		return workerPipe{}, err
	}
	done := make(chan struct{})
	go func() {
		cmd.Wait()
		close(done)
	}()
	return workerPipe{in: in, out: out, kill: func() { cmd.Process.Kill() }, done: done}, nil
}
