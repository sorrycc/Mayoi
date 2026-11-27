package remote

import (
	"bufio"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf16"
)

// A Windows host runs Windows PowerShell, which every Windows since 10
// has, in place of sh: nothing is installed there either. Its folders
// keep the editor's slash paths, the drive first, as /C:/Users/me; the
// scripts turn them into C:\Users\me and back. Scripts write to standard
// output as bytes, never through PowerShell's own output, which would
// turn them into text.
//
// The scripts are written without PowerShell's backtick escapes, which
// a Go raw string cannot hold: $Z is a NUL and $NL a newline.

// psBootstrap is what powershell runs: it reads the length of the script,
// then the script, from standard input, leaves the rest of it to the
// script, and runs it. What the script lets fall to PowerShell's output
// is dropped.
const psBootstrap = `$ErrorActionPreference='Stop';$i=[Console]::OpenStandardInput();$n=0;` +
	`while($true){$c=$i.ReadByte();if($c -lt 0){exit 1};if($c -eq 10){break};$n=$n*10+$c-48};` +
	`$b=New-Object byte[] $n;$o=0;while($o -lt $n){$r=$i.Read($b,$o,$n-$o);if($r -le 0){exit 1};$o+=$r};` +
	`$global:MI=$i;try{$null=. ([ScriptBlock]::Create([Text.Encoding]::UTF8.GetString($b)))}` +
	`catch{$e=[Text.Encoding]::UTF8.GetBytes($_.Exception.Message+[char]10);$s=[Console]::OpenStandardError();$s.Write($e,0,$e.Length);exit 1};exit 0`

// psArgs are the arguments of powershell that run psBootstrap. Both
// cmd.exe and PowerShell, the shells sshd on Windows may start, take
// them as they are.
var psArgs = "-NoProfile -NonInteractive -ExecutionPolicy Bypass -EncodedCommand " + encodePS(psBootstrap)

// psLauncher is the command ssh has a Windows host run, as launcher is
// on other hosts.
var psLauncher = "powershell " + psArgs

// encodePS is a script as -EncodedCommand takes it: UTF-16 in base64.
func encodePS(script string) string {
	u := utf16.Encode([]rune(script))
	b := make([]byte, 2*len(u))
	for i, v := range u {
		b[2*i], b[2*i+1] = byte(v), byte(v>>8)
	}
	return base64.StdEncoding.EncodeToString(b)
}

// psPreamble starts every script of a Windows host. $A are the arguments.
// W writes text, Fail says why and ends with a code, N turns an editor
// path into one of the host and P back, St prints the line of statCmd of
// a file, and CopyN copies n bytes, all of them when n is below 0.
const psPreamble = `$ProgressPreference='SilentlyContinue'
$MO=[Console]::OpenStandardOutput()
$ME=[Console]::OpenStandardError()
$U8=New-Object Text.UTF8Encoding $false
$Z=[string][char]0
$NL=[string][char]10
$Sep=[IO.Path]::DirectorySeparatorChar
$IsWin=$Sep -eq [char]92
function W([string]$s){$x=$U8.GetBytes($s);$MO.Write($x,0,$x.Length)}
function Fail([string]$m,[int]$c=1){$MO.Flush();$x=$U8.GetBytes($m+$NL);$ME.Write($x,0,$x.Length);exit $c}
function D([string]$s){$U8.GetString([Convert]::FromBase64String($s))}
function N([string]$p){if($p -match '^/[A-Za-z]:(/|$)'){$p=$p.Substring(1)};if($p -match '^[A-Za-z]:$'){$p+='/'};$p.Replace([char]47,$Sep)}
function P([string]$p){$p=$p.Replace([char]92,[char]47);if(-not $p.StartsWith('/')){$p='/'+$p};$p}
function Ex([string]$p){[IO.File]::Exists($p) -or [IO.Directory]::Exists($p)}
function St([string]$k){$p=N $k;try{$i=Get-Item -LiteralPath $p -Force -ErrorAction Stop}catch{return ''}
$t=$i.LastWriteTimeUtc.Ticks-621355968000000000;$s=[long][Math]::Floor($t/10000000);$f=[long]($t%10000000)
if($i.PSIsContainer){$m=0x41ed;$z=0}else{$m=0x81a4;$z=$i.Length;if($i.IsReadOnly){$m=0x8124}}
return ('{0} {1}.{2} {3:x}|{4}' -f $z,$s,$f.ToString('D7'),$m,$k)+$NL}
function CopyN($from,$to,[long]$n,[bool]$whole=$false){$buf=New-Object byte[] 65536
while($n -ne 0){$want=65536;if($n -gt 0 -and $n -lt $want){$want=[int]$n};$r=$from.Read($buf,0,$want);if($r -le 0){break};$to.Write($buf,0,$r);if($n -gt 0){$n-=$r}}
if($whole -and $n -gt 0){Fail 'the files did not arrive whole'}}
function RL{$ms=New-Object IO.MemoryStream;while($true){$c=$MI.ReadByte();if($c -lt 0){return $null};if($c -eq 0){break};$ms.WriteByte([byte]$c)};$U8.GetString($ms.ToArray())}
function RmTree([string]$d){foreach($i in @(Get-ChildItem -LiteralPath $d -Force)){$l=($i.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0
if($i.PSIsContainer -and -not $l){RmTree $i.FullName}elseif($i.PSIsContainer){[IO.Directory]::Delete($i.FullName)}else{$i.Attributes='Normal';[IO.File]::Delete($i.FullName)}}
[IO.Directory]::Delete($d)}
function Ls([string]$k){W ($k+$Z)
if($IsWin -and $k -eq '/'){foreach($d in [IO.DriveInfo]::GetDrives()){if($d.IsReady){W ('d '+$d.Name.Substring(0,2)+$Z)}};W $Z;return}
$p=N $k
if(-not [IO.Directory]::Exists($p)){if([IO.File]::Exists($p)){W ('!not a directory'+$Z)}else{W ('!no such file or directory'+$Z)};return}
try{$e=@(Get-ChildItem -LiteralPath $p -Force -ErrorAction Stop)}catch{W ('!permission denied'+$Z);return}
foreach($i in $e){if($i.PSIsContainer){$c='d'}else{$c='f'};if($i.Attributes -band [IO.FileAttributes]::ReparsePoint){$c=$c.ToUpper()};W ($c+' '+$i.Name+$Z)}
W $Z}
function Free([string]$dir,[string]$n,[bool]$isDir){if(-not (Ex ([IO.Path]::Combine($dir,$n)))){return $n}
$stem=$n;$ext='';if(-not $isDir){$x=$n.LastIndexOf('.');if($x -gt 0){$stem=$n.Substring(0,$x);$ext=$n.Substring($x)}}
$k=1;while($true){if($k -eq 1){$m=$stem+' copy'+$ext}else{$m=$stem+' copy '+$k+$ext};if(-not (Ex ([IO.Path]::Combine($dir,$m)))){return $m};$k++}}
`

// psScript is the text of one command of a Windows host: the preamble,
// then its arguments, then script. PowerShell's names know no case, so no
// variable of a script is $a, nor one of the preamble's, as $z.
func psScript(script string, args []string) string {
	var b strings.Builder
	b.WriteString(psPreamble)
	b.WriteString("$A=@(")
	for i, a := range args {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString("(D '")
		b.WriteString(base64.StdEncoding.EncodeToString([]byte(a)))
		b.WriteString("')")
	}
	b.WriteString(")\n")
	b.WriteString(script)
	return b.String()
}

// psProbe says what a Windows host is and has, as probeScript does.
const psProbe = `W ((P $HOME)+$NL+'Windows'+$NL)
foreach($t in 'git','rg'){if(Get-Command $t -CommandType Application -ErrorAction SilentlyContinue){W ('have:'+$t+$NL)}}
`

// psStatAll prints the line of St of each argument there is.
const psStatAll = `foreach($k in $A){W (St $k)}
`

// psReadDirs lists each folder named, as readDirScript does.
const psReadDirs = `foreach($k in $A){Ls $k}
`

// psReadFile writes the file $A[0], at most $A[1] bytes, all with -1.
const psReadFile = `$k=$A[0];$p=N $k
if([IO.Directory]::Exists($p)){Fail ($k+': is a directory')}
if(-not [IO.File]::Exists($p)){Fail ($k+': No such file or directory')}
$f=[IO.File]::Open($p,'Open','Read','ReadWrite');try{CopyN $f $MO ([long]$A[1])}finally{$f.Close()}
`

// psReadFileInfo writes the line of St of the file $A[0], then at most
// $A[1] bytes of it.
const psReadFileInfo = `$k=$A[0];$p=N $k
if([IO.Directory]::Exists($p)){Fail ($k+': is a directory')}
if(-not [IO.File]::Exists($p)){Fail ($k+': No such file or directory')}
$f=[IO.File]::Open($p,'Open','Read','ReadWrite');try{W (St $k);CopyN $f $MO ([long]$A[1])}finally{$f.Close()}
`

// psWriteFile is the script of WriteFileIf: $A[0] the file, $A[1] the
// bytes, $A[2] the stamp it must have. The file is opened, not made
// again, so that it keeps what Windows keeps of it.
const psWriteFile = `$k=$A[0];$p=N $k
if($A[2] -ne '' -and (Ex $p)){$cur=((St $k) -split ' ')[0..1] -join ' ';if($cur -cne $A[2]){$MI.CopyTo([IO.Stream]::Null);Fail '` + changedMark + `' 3}}
$t=[IO.Path]::GetTempFileName()
try{$o=[IO.File]::Create($t);try{$MI.CopyTo($o)}finally{$o.Close()}
if((New-Object IO.FileInfo $t).Length -ne [long]$A[1]){Fail 'the file did not arrive whole'}
$o=[IO.File]::Open($p,'OpenOrCreate','Write','Read');try{$o.SetLength(0);$s=[IO.File]::OpenRead($t);try{$s.CopyTo($o)}finally{$s.Close()}}finally{$o.Close()}
}finally{[IO.File]::Delete($t)}
W (St $k)
`

// psRename moves $A[0] to $A[1], unless a file is there: one that only
// differs in case is the same file on Windows.
const psRename = `$from=N $A[0];$to=N $A[1]
if($A[0] -ne $A[1] -and (Ex $to)){Fail ($A[1]+': file exists')}
if([IO.Directory]::Exists($from)){[IO.Directory]::Move($from,$to)}else{[IO.File]::Move($from,$to)}
`

// psRemove deletes $A[0], and all in it, never going through a link.
const psRemove = `$p=N $A[0]
$i=Get-Item -LiteralPath $p -Force -ErrorAction SilentlyContinue
if($i -eq $null){exit 0}
$l=($i.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0
if($i.PSIsContainer -and -not $l){RmTree $p}elseif($i.PSIsContainer){[IO.Directory]::Delete($p)}else{$i.Attributes='Normal';[IO.File]::Delete($p)}
`

const psMkdirAll = `$null=[IO.Directory]::CreateDirectory((N $A[0]))
`

const psCreate = `$p=N $A[0]
if(Ex $p){Fail ($A[0]+': file exists')}
$null=[IO.Directory]::CreateDirectory([IO.Path]::GetDirectoryName($p));$f=[IO.File]::Open($p,'CreateNew');$f.Close()
`

// psReadFiles packs the files $A[2..] of the folder $A[0] smaller than
// $A[1] bytes, as writePack does.
const psReadFiles = `$d=N $A[0];$max=[long]$A[1]
for($j=2;$j -lt $A.Count;$j++){$r=$A[$j];$p=[IO.Path]::Combine($d,$r.Replace([char]47,$Sep))
if(-not [IO.File]::Exists($p)){continue};$i=New-Object IO.FileInfo $p;if($i.Length -ge $max){continue}
try{$f=[IO.File]::Open($p,'Open','Read','ReadWrite')}catch{continue}
try{W ('F'+$r+$Z+$i.Length+$Z);CopyN $f $MO $i.Length $true}finally{$f.Close()}}
`

// psLook lists the first $A[0] folders, then statMark, then stats the
// files after them, as the script of Look does.
const psLook = `$n=[int]$A[0]
for($j=1;$j -le $n;$j++){Ls $A[$j]}
W ([string][char]1+'mayoi-stat'+[char]1+$NL)
for(;$j -lt $A.Count;$j++){W (St $A[$j])}
`

// psStatUnder stats the files $A[1..], then the folder $A[0] and every
// folder under it.
const psStatUnder = `$d=$A[0]
for($j=1;$j -lt $A.Count;$j++){W (St $A[$j])}
$r=N $d
if([IO.Directory]::Exists($r)){W (St $d)
$base=$r.TrimEnd([char]92,[char]47).Length
foreach($i in @(Get-ChildItem -LiteralPath $r -Recurse -Directory -Force -ErrorAction SilentlyContinue)){W (St ($d+$i.FullName.Substring($base).Replace([char]92,[char]47)))}}
`

// psUpload unpacks what writePack sends into a folder of its own in the
// folder $A[0], a new one of the temporary ones when it is "", and moves
// each of $A[1..] into it under a free name, printing where it went.
const psUpload = `$d=$A[0]
if($d -eq ''){$d=P ([IO.Path]::Combine([IO.Path]::GetTempPath(),'mayoi-paste.'+[Guid]::NewGuid().ToString('N').Substring(0,8)));$null=[IO.Directory]::CreateDirectory((N $d))}
$nd=N $d
$s=[IO.Path]::Combine($nd,'.mayoi-paste.'+[Guid]::NewGuid().ToString('N').Substring(0,8))
$null=[IO.Directory]::CreateDirectory($s)
try{
while($true){$c=$MI.ReadByte();if($c -lt 0){break};$r=RL;if($r -eq $null){Fail 'the files did not arrive whole'}
$q=[IO.Path]::Combine($s,$r.Replace([char]47,$Sep))
if($c -eq 68){$null=[IO.Directory]::CreateDirectory($q)}else{$size=[long](RL);$o=[IO.File]::Create($q);try{CopyN $MI $o $size $true}finally{$o.Close()}}}
for($j=1;$j -lt $A.Count;$j++){$n=$A[$j];$from=[IO.Path]::Combine($s,[string]($j-1),$n)
$isDir=[IO.Directory]::Exists($from);$to=Free $nd $n $isDir;$dst=[IO.Path]::Combine($nd,$to)
if($isDir){[IO.Directory]::Move($from,$dst)}else{[IO.File]::Move($from,$dst)}
W ($d.TrimEnd([char]47)+'/'+$to+$Z)}
}finally{if([IO.Directory]::Exists($s)){RmTree $s}}
`

// psCopy copies each of $A[1..] of the host into its folder $A[0], under
// a free name, printing where it went.
const psCopy = `$d=$A[0];$nd=N $d
for($j=1;$j -lt $A.Count;$j++){$from=N $A[$j];$isDir=[IO.Directory]::Exists($from)
$to=Free $nd ([IO.Path]::GetFileName($from.TrimEnd([char]92,[char]47))) $isDir
Copy-Item -LiteralPath $from -Destination ([IO.Path]::Combine($nd,$to)) -Recurse -Force
W ($d.TrimEnd([char]47)+'/'+$to+$Z)}
`

// psDownload packs the file or folder $A[0], as writePack does, under its
// name. A link to a folder goes as an empty folder.
const psDownload = `function Pk([string]$p,[string]$rel){$i=Get-Item -LiteralPath $p -Force
if($i.PSIsContainer){W ('D'+$rel+$Z);if($i.Attributes -band [IO.FileAttributes]::ReparsePoint){return}
foreach($c in @(Get-ChildItem -LiteralPath $p -Force)){Pk $c.FullName ($rel+'/'+$c.Name)}}
else{$f=[IO.File]::Open($p,'Open','Read','ReadWrite');try{W ('F'+$rel+$Z+$i.Length+$Z);CopyN $f $MO $i.Length $true}finally{$f.Close()}}}
$p=N $A[0]
if(-not (Ex $p)){Fail ($A[0]+': No such file or directory')}
Pk $p ([IO.Path]::GetFileName($p.TrimEnd([char]92,[char]47)))
`

// psExec runs the program $A[1] in the folder $A[0] with the command line
// $A[2], without the variables named by the $A[3] after it and with the
// variables after those, its standard streams those of the script. A
// .cmd or .bat goes through cmd.exe, as npm installs programs. A variable
// is taken out with a null: PowerShell makes $null "", which .NET 9 and
// later keep as an empty variable.
const psExec = `$p=N $A[0]
if(-not [IO.Directory]::Exists($p)){Fail ($A[0]+': No such file or directory')}
[Environment]::CurrentDirectory=$p
$u=[int]$A[3];$j=4
for(;$j -lt 4+$u;$j++){[Environment]::SetEnvironmentVariable($A[$j],[NullString]::Value)}
for(;$j -lt $A.Count;$j++){$kv=$A[$j];$x=$kv.IndexOf('=');[Environment]::SetEnvironmentVariable($kv.Substring(0,$x),$kv.Substring($x+1))}
$exe=$A[1]
if(-not ($exe.Contains('/') -or $exe.Contains([string][char]92))){$c=Get-Command $exe -CommandType Application -ErrorAction SilentlyContinue|Select-Object -First 1
if($c -eq $null){Fail ($exe+': command not found') 127};$exe=$c.Path}else{$exe=N $exe}
$si=New-Object Diagnostics.ProcessStartInfo
if($exe -match '[.](cmd|bat)$'){$si.FileName=$env:ComSpec;$si.Arguments='/d /s /c "'+[char]34+$exe+[char]34+' '+$A[2]+'"'}else{$si.FileName=$exe;$si.Arguments=$A[2]}
$si.UseShellExecute=$false;$si.WorkingDirectory=$p
$pr=[Diagnostics.Process]::Start($si);$pr.WaitForExit();exit $pr.ExitCode
`

// psFindProgram prints mayoi:found when the program $A[1] runs in the
// folder $A[0], with the variables of psExec, and mayoi:missing if not.
const psFindProgram = `$u=[int]$A[2];$j=3
for(;$j -lt 3+$u;$j++){[Environment]::SetEnvironmentVariable($A[$j],[NullString]::Value)}
for(;$j -lt $A.Count;$j++){$kv=$A[$j];$x=$kv.IndexOf('=');[Environment]::SetEnvironmentVariable($kv.Substring(0,$x),$kv.Substring($x+1))}
$exe=$A[1]
if($exe.Contains('/') -or $exe.Contains([string][char]92)){$ok=[IO.File]::Exists((N $exe))}else{$ok=$null -ne (Get-Command $exe -CommandType Application -ErrorAction SilentlyContinue)}
if($ok){W ('mayoi:found'+$NL)}else{W ('mayoi:missing'+$NL)}
`

// psWalk prints the files under the folder $A[0], relative to it with
// slashes, each followed by a NUL, at most $A[1] of them, leaving out the
// folders named $A[2..] and not going through links.
const psWalk = `$r=N $A[0];$max=[int]$A[1];$skip=@{}
for($j=2;$j -lt $A.Count;$j++){$skip[$A[$j]]=$true}
$base=$r.TrimEnd([char]92,[char]47).Length+1;$n=0
$st=New-Object Collections.Stack;$st.Push($r)
while($st.Count -gt 0 -and $n -lt $max){$d=$st.Pop()
try{$e=[IO.Directory]::GetFileSystemEntries($d)}catch{continue}
foreach($p in $e){$at=[IO.File]::GetAttributes($p)
if($at -band [IO.FileAttributes]::Directory){if(-not ($at -band [IO.FileAttributes]::ReparsePoint) -and -not $skip.ContainsKey([IO.Path]::GetFileName($p))){$st.Push($p)}}
else{W ($p.Substring($base).Replace([char]92,[char]47)+$Z);$n++}}}
`

// psFrameServer runs the commands that frames bring, as frameShell does,
// each in a powershell of its own that psBootstrap starts, with $BOOT the
// arguments of that powershell. Standard input, output and error of the
// command go as they come, all at once, so that neither waits on the
// other.
const psFrameServer = `$exe=(Get-Process -Id $PID).Path
$ib=New-Object byte[] 65536;$script:ip=0;$script:il=0
function NB{if($script:ip -ge $script:il){$script:il=$MI.Read($ib,0,$ib.Length);$script:ip=0;if($script:il -le 0){exit 0}};$b=$ib[$script:ip];$script:ip++;return $b}
function Hdr{$sb=New-Object Text.StringBuilder;while($true){$c=NB;if($c -eq 10){break};$null=$sb.Append([char]$c)};$sb.ToString().TrimEnd([char]13)}
function Frame([string]$k,[byte[]]$b,[int]$n){W ($k+' '+$n+$NL);if($n -gt 0){$MO.Write($b,0,$n)};$MO.Flush()}
while($true){$h=Hdr;$k=$h.Substring(0,1);$n=[int]$h.Substring(2);if($k -ne 'C'){exit 1}
$sc=New-Object byte[] $n;for($j=0;$j -lt $n;$j++){$sc[$j]=NB}
$si=New-Object Diagnostics.ProcessStartInfo $exe
$si.Arguments=$BOOT;$si.UseShellExecute=$false;$si.CreateNoWindow=$true
$si.RedirectStandardInput=$true;$si.RedirectStandardOutput=$true;$si.RedirectStandardError=$true
$p=[Diagnostics.Process]::Start($si);$cin=$p.StandardInput.BaseStream
$hd=$U8.GetBytes([string]$n+$NL);$cin.Write($hd,0,$hd.Length);$cin.Write($sc,0,$n);$cin.Flush()
$ob=New-Object byte[] 32768;$eb=New-Object byte[] 32768
$ot=$p.StandardOutput.BaseStream.ReadAsync($ob,0,$ob.Length);$et=$p.StandardError.BaseStream.ReadAsync($eb,0,$eb.Length)
$rt=$null;$wt=$null;$ended=$false;$drop=$false;$need=-1;$closed=$false
while($ot -ne $null -or $et -ne $null -or -not $ended -or $wt -ne $null){
if($wt -eq $null -and -not $ended -and $rt -eq $null){
if($script:ip -lt $script:il){
if($need -lt 0){$ih=Hdr;$ik=$ih.Substring(0,1);$need=[int]$ih.Substring(2)
if($ik -eq 'E'){$ended=$true;$need=-1;if(-not $closed){try{$cin.Close()}catch{};$closed=$true};continue};if($ik -ne 'I'){exit 1};if($need -eq 0){$need=-1};continue}
$m=[Math]::Min($need,$script:il-$script:ip)
if($drop){$script:ip+=$m}else{$wt=$cin.WriteAsync($ib,$script:ip,$m);$script:ip+=$m}
$need-=$m;if($need -eq 0){$need=-1};continue}
$rt=$MI.ReadAsync($ib,0,$ib.Length)}
$ts=@();foreach($t in @($ot,$et,$rt,$wt)){if($t -ne $null){$ts+=$t}}
$null=[Threading.Tasks.Task]::WaitAny([Threading.Tasks.Task[]]$ts)
if($rt -ne $null -and $rt.IsCompleted){$script:il=$rt.Result;$script:ip=0;$rt=$null;if($script:il -le 0){exit 0}}
if($wt -ne $null -and $wt.IsCompleted){if($wt.IsFaulted){$drop=$true};$wt=$null}
if($ot -ne $null -and $ot.IsCompleted){$r=0;if(-not $ot.IsFaulted){$r=$ot.Result};if($r -le 0){$ot=$null}else{Frame 'O' $ob $r;$ot=$p.StandardOutput.BaseStream.ReadAsync($ob,0,$ob.Length)}}
if($et -ne $null -and $et.IsCompleted){$r=0;if(-not $et.IsFaulted){$r=$et.Result};if($r -le 0){$et=$null}else{Frame 'R' $eb $r;$et=$p.StandardError.BaseStream.ReadAsync($eb,0,$eb.Length)}}
if($ot -eq $null -and $et -eq $null -and -not $closed -and $p.HasExited){$drop=$true}}
$p.WaitForExit();$x=$U8.GetBytes([string]$p.ExitCode);Frame 'X' $x $x.Length;$p.Dispose()}
`

// psFrameServerScript is psFrameServer with the arguments its commands'
// powershell takes.
func psFrameServerScript() string {
	return "$BOOT='" + psArgs + "'\n" + psPreamble + psFrameServer
}

// winArg quotes an argument for a Windows command line, as programs
// built with the C runtime part it.
func winArg(s string) string {
	if s == "" {
		return `""`
	}
	if !strings.ContainsAny(s, " \t\"") {
		return s
	}
	var b strings.Builder
	b.WriteByte('"')
	slashes := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			slashes++
		case '"':
			for ; slashes > 0; slashes-- {
				b.WriteByte('\\')
			}
			b.WriteByte('\\')
		default:
			slashes = 0
		}
		b.WriteByte(s[i])
	}
	for ; slashes > 0; slashes-- {
		b.WriteByte('\\')
	}
	b.WriteByte('"')
	return b.String()
}

// psQuote quotes s for PowerShell: it takes curly quotes as quotes too.
func psQuote(s string) string {
	r := strings.NewReplacer("'", "''", "\u2018", "\u2018\u2018", "\u2019", "\u2019\u2019", "\u201a", "\u201a\u201a", "\u201b", "\u201b\u201b")
	return "'" + r.Replace(s) + "'"
}

// isDrivePath reports whether p is a path of a Windows drive, as
// /C:/Users or /C:.
func isDrivePath(p string) bool {
	return len(p) >= 3 && p[0] == '/' && p[2] == ':' && isLetter(p[1]) && (len(p) == 3 || p[3] == '/')
}

func isLetter(c byte) bool { return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' }

// hostDrivePath returns a path a program of a Windows host wrote, as
// C:\Users\me or C:/Users/me, as the editor knows it there, /C:/Users/me;
// false when p is not one.
func hostDrivePath(p string) (string, bool) {
	if len(p) < 2 || p[1] != ':' || !isLetter(p[0]) || len(p) > 2 && p[2] != '/' && p[2] != '\\' {
		return "", false
	}
	return path.Clean("/" + strings.ToUpper(p[:1]) + strings.ReplaceAll(p[1:], `\`, "/")), true
}

// WindowsPath is a path of a Windows host as Windows spells it, as
// C:\Users\me for /C:/Users/me. Any other path stays as it is.
func WindowsPath(p string) string {
	if !isDrivePath(p) {
		return p
	}
	p = strings.ReplaceAll(p[1:], "/", `\`)
	if len(p) == 2 {
		p += `\`
	}
	return p
}

// hostArg is an argument of a program of a Windows host: a path of a
// drive goes as Windows takes it.
func hostArg(a string) string {
	if isDrivePath(a) {
		return a[1:]
	}
	return a
}

// psExecArgs are the arguments of psExec for a program of a Windows host.
func psExecArgs(dir string, env, unset []string, name string, args []string) []string {
	line := make([]string, len(args))
	for i, a := range args {
		line[i] = winArg(hostArg(a))
	}
	out := []string{dir, name, strings.Join(line, " "), strconv.Itoa(len(unset))}
	out = append(out, unset...)
	return append(out, env...)
}

// Files go to and from a Windows host, which may have no tar, as a pack:
// a "D" and the path of a folder, or an "F", the path of a file, its
// size and then its bytes, each part ended by a NUL but the bytes.

// writePack writes the files and folders srcs as a pack, the one at i as
// "i/name", as writeTar does. Links stay behind.
func writePack(w io.Writer, srcs []string) error {
	bw := bufio.NewWriter(w)
	for i, src := range srcs {
		top := strconv.Itoa(i)
		fmt.Fprintf(bw, "D%s\x00", top)
		base := filepath.Dir(src)
		err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(base, p)
			if err != nil {
				return err
			}
			name := top + "/" + filepath.ToSlash(rel)
			switch {
			case d.IsDir():
				fmt.Fprintf(bw, "D%s\x00", name)
			case d.Type().IsRegular():
				info, err := d.Info()
				if err != nil {
					return err
				}
				f, err := os.Open(p)
				if err != nil {
					return err
				}
				defer f.Close()
				fmt.Fprintf(bw, "F%s\x00%d\x00", name, info.Size())
				if _, err := io.CopyN(bw, f, info.Size()); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return bw.Flush()
}

// eachPacked reads a pack, giving fn each entry; a file's bytes are to be
// read from r before the next.
func eachPacked(r *bufio.Reader, fn func(dir bool, name string, size int64, r io.Reader) error) error {
	for {
		kind, err := r.ReadByte()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name, err := r.ReadString(0)
		if err != nil {
			return io.ErrUnexpectedEOF
		}
		name = strings.TrimSuffix(name, "\x00")
		switch kind {
		case 'D':
			if err := fn(true, name, 0, nil); err != nil {
				return err
			}
		case 'F':
			ns, err := r.ReadString(0)
			if err != nil {
				return io.ErrUnexpectedEOF
			}
			size, err := strconv.ParseInt(strings.TrimSuffix(ns, "\x00"), 10, 64)
			if err != nil || size < 0 {
				return errors.New("a bad pack from the host")
			}
			body := io.LimitReader(r, size)
			if err := fn(false, name, size, body); err != nil {
				return err
			}
			if n, _ := io.Copy(io.Discard, body); n > 0 {
				return io.ErrUnexpectedEOF
			}
		default:
			return errors.New("a bad pack from the host")
		}
	}
}

// readPack unpacks a pack of the entry name at to, as readTar does.
func readPack(r io.Reader, name, to string) error {
	got := false
	err := eachPacked(bufio.NewReader(r), func(dir bool, rel string, size int64, body io.Reader) error {
		rel = path.Clean(rel)
		var dst string
		switch {
		case rel == name:
			dst = to
		case strings.HasPrefix(rel, name+"/") && !strings.Contains("/"+rel+"/", "/../"):
			dst = filepath.Join(to, filepath.FromSlash(strings.TrimPrefix(rel, name+"/")))
		default:
			return nil
		}
		got = true
		if dir {
			if err := os.Mkdir(dst, 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
				return err
			}
			return nil
		}
		f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return err
		}
		n, err := io.Copy(f, body)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err == nil && n != size {
			err = io.ErrUnexpectedEOF
		}
		return err
	})
	if err != nil {
		return err
	}
	if !got {
		return &fs.PathError{Op: "copy", Path: name, Err: fs.ErrNotExist}
	}
	return nil
}

// Walk lists the files under the folder dir of a Windows host, relative to
// it with slashes, at most max of them, leaving out the folders named
// skip, as find does on other hosts.
func (c *Conn) Walk(dir string, skip []string, max int) ([]string, error) {
	out, err := c.Run(nil, psWalk, append([]string{dir, strconv.Itoa(max)}, skip...)...)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, f := range strings.Split(string(out), "\x00") {
		if f != "" {
			files = append(files, f)
		}
	}
	return files, nil
}
