package server

// remoteAccessAllowed gates the in-browser remote access features: the web
// SSH terminal and RDP (via guacd).
//
// Fork policy (BTLzdravtech): both are switched off. PatchMon holds agent
// connections to every managed host, so a server that can also open shells
// and desktop sessions on them is a much larger security burden than a patch
// manager needs to be.
//
// When this returns false the router builds neither the SSH ticket store nor
// the RDP handler. Everything downstream already keys off those being nil:
// the ticket endpoints and WebSocket tunnels are not registered (404), the
// agent WebSocket handler gets no SSH/RDP callbacks, and guacd is not started.
// The UI is deliberately left alone; its Terminal and RDP actions simply fail.
//
// Kept in its own file, with two one-line uses in router.go, so upstream
// merges stay trivial.
func remoteAccessAllowed() bool { return false }
