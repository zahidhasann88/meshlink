Output docs/demo.gif

Set Shell bash
Set FontSize 15
Set Width 1300
Set Height 470
Set Padding 20
Set TypingSpeed 30ms
Set Theme "Catppuccin Mocha"

Hide
Type "export PATH=/vhs/dist/demo-bin:$PATH PS1='$ ' && cd $(mktemp -d) && clear"
Enter
Show

Type "# 1. Create an identity for each peer"
Enter
Type "A=$(meshlink identity create --key a.key); B=$(meshlink identity create --key b.key)"
Enter
Sleep 500ms

Type "# 2. Start two nodes that only allow each other"
Enter
Type "(meshlink node start --key b.key --listen 127.0.0.1:7002 --allow $A --socket $PWD/b.sock --echo &> b.log &)"
Enter
Sleep 700ms
Type "(meshlink node start --key a.key --listen 127.0.0.1:7001 --allow $B --socket $PWD/a.sock &> a.log &)"
Enter
Sleep 700ms
Type "export MESHLINK_SOCKET=$PWD/a.sock"
Enter

Type "# 3. Connect over mutual TLS 1.3 and send a request"
Enter
Type "meshlink peer connect --peer $B 127.0.0.1:7002"
Enter
Sleep 600ms
Type "meshlink message request $B 'hello, peer to peer'"
Enter
Sleep 800ms
Type "meshlink peer list"
Enter
Sleep 600ms
Type "meshlink diagnostics"
Enter
Sleep 4s

Hide
Type "meshlink node stop; meshlink node stop --socket $PWD/b.sock; sleep 1"
Enter
