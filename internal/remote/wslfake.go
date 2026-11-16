package remote

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

func init() {
	// A test copies this binary to wsl.exe and sets the env. The check is
	// the file name, so a normal mayoi process is left alone.
	if os.Getenv("MAYOI_FAKE_WSL") != "1" {
		return
	}
	base := strings.ToLower(filepath.Base(os.Args[0]))
	if base != "wsl" && base != "wsl.exe" {
		return
	}
	os.Exit(fakeWSL(os.Args[1:]))
}

// fakeWSL stands in for wsl.exe. -l -q prints the UTF-16 list from
// MAYOI_FAKE_WSL_NAMES. Anything else reads the bootstrap script from
// stdin and then speaks frames. MAYOI_FAKE_WSL_STOP, when that file
// exists, is a distribution that will not stay up.
func fakeWSL(args []string) int {
	if len(args) > 0 && args[0] == "-l" {
		if err := writeUTF16List(os.Stdout, os.Getenv("MAYOI_FAKE_WSL_NAMES")); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	if dir := os.Getenv("MAYOI_FAKE_WSL_LOG"); dir != "" {
		_ = os.MkdirAll(dir, 0o755)
		_ = os.WriteFile(filepath.Join(dir, strconv.Itoa(os.Getpid())), []byte(strings.Join(args, "\n")), 0o644)
	}
	if wslStopped() {
		fmt.Fprintln(os.Stderr, wslUnavailable)
		return 1
	}
	go watchWSLStop()
	in := bufio.NewReader(os.Stdin)
	if err := readBootstrap(in); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Fprintln(os.Stdout, wslReadyLine)
	err := serveFrames(readWriter{in, os.Stdout}, fakeWSLRun)
	if err != nil && !isPipeEnd(err) {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func wslStopped() bool {
	path := os.Getenv("MAYOI_FAKE_WSL_STOP")
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func watchWSLStop() {
	if os.Getenv("MAYOI_FAKE_WSL_STOP") == "" {
		return
	}
	for {
		if wslStopped() {
			fmt.Fprintln(os.Stderr, wslUnavailable)
			os.Exit(1)
		}
		time.Sleep(30 * time.Millisecond)
	}
}

func readBootstrap(r *bufio.Reader) error {
	line, err := r.ReadString('\n')
	if err != nil {
		return err
	}
	n, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || n < 0 {
		return fmt.Errorf("wsl bootstrap")
	}
	_, err = io.CopyN(io.Discard, r, int64(n))
	return err
}

func fakeWSLRun(script string, in io.Reader, stdout, stderr io.Writer) int {
	if strings.Contains(script, "uname -s") {
		io.WriteString(stdout, "/home/me\nLinux\n")
		return 0
	}
	io.Copy(stdout, in)
	return 0
}

func writeUTF16List(w io.Writer, names string) error {
	encoded := utf16.Encode([]rune(names))
	buf := make([]byte, 2+len(encoded)*2)
	buf[0], buf[1] = 0xFF, 0xFE
	for i, u := range encoded {
		binary.LittleEndian.PutUint16(buf[2+i*2:], u)
	}
	_, err := w.Write(buf)
	return err
}

type readWriter struct {
	io.Reader
	io.Writer
}

func isPipeEnd(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe)
}
