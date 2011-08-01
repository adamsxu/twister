package main

import (
	"github.com/garyburd/twister/web"
	"github.com/garyburd/twister/websocket"
	"github.com/garyburd/twister/server"
	"log"
	"template"
)

type subscription struct {
	conn      *websocket.Conn
	subscribe bool
}

var (
	messageChan      = make(chan []byte)
	chatTempl        *template.Template
	subscriptionChan = make(chan subscription)
)

func hub() {
	conns := make(map[*websocket.Conn]int)
	for {
		select {
		case subscription := <-subscriptionChan:
			conns[subscription.conn] = 0, subscription.subscribe
		case message := <-messageChan:
			for conn, _ := range conns {
				if err := conn.WriteMessage(message); err != nil {
					conn.Close()
				}
			}
		}
	}
}

func chatWsHandler(req *web.Request) {
	conn, err := websocket.Upgrade(req, 1024, 1024, nil)
	if err != nil {
		log.Print("Upgrade failed", err)
		return
	}

	defer func() {
		subscriptionChan <- subscription{conn, false}
		conn.Close()
	}()

	subscriptionChan <- subscription{conn, true}

	for {
		p, hasMore, err := conn.ReadMessage()
		if err != nil || hasMore {
			log.Println("Exiting read loop, err:", err, " hasMore:", hasMore)
			break
		}
		// copy because Receive reuses underling byte array.
		mp := make([]byte, len(p))
		copy(mp, p)
		messageChan <- mp
	}
}

func chatFrameHandler(req *web.Request) {
	chatTempl.Execute(
		req.Respond(web.StatusOK, web.HeaderContentType, "text/html; charset=utf-8"),
		req.URL.Host)
}

func main() {
	chatTempl = template.New(template.FormatterMap{"": template.HTMLFormatter})
	chatTempl.SetDelims("{{", "}}")
	if err := chatTempl.Parse(chatStr); err != nil {
		panic("template error: " + err.String())
	}
	go hub()
	server.Run(":8080",
		web.NewRouter().
			Register("/", "GET", chatFrameHandler).
			Register("/ws", "GET", chatWsHandler))
}

const chatStr = `
<html>
<head>
<title>Chat Example</title>
<script type="text/javascript" src="http://ajax.googleapis.com/ajax/libs/jquery/1.4.2/jquery.min.js"></script>
<script type="text/javascript">
    $(function() {

    var conn;
    var msg = $("#msg");
    var log = $("#log");

    function appendLog(msg) {
        var d = log[0]
        var doScroll = d.scrollTop == d.scrollHeight - d.clientHeight;
        msg.appendTo(log)
        if (doScroll) {
            d.scrollTop = d.scrollHeight - d.clientHeight;
        }
    }

    $("#form").submit(function() {
        if (!conn) {
            return false;
        }
        if (!msg.val()) {
            return false;
        }
        conn.send(msg.val());
        msg.val("");
        return false
    });

    if (window["WebSocket"]) {
        conn = new WebSocket("ws://{{@}}/ws");
        conn.onclose = function(evt) {
            appendLog($("<div><b>Connection closed.</b></div>"))
        }
        conn.onmessage = function(evt) {
            appendLog($("<div/>").text(evt.data))
        }
    } else {
        appendLog($("<div><b>Your browser does not support WebSockets.</b></div>"))
    }
    });
</script>
<style type="text/css">
html {
    overflow: hidden;
}

body {
    overflow: hidden;
    padding: 0;
    margin: 0;
    width: 100%;
    height: 100%;
    background: gray;
}

#log {
    background: white;
    margin: 0;
    padding: 0.5em 0.5em 0.5em 0.5em;
    position: absolute;
    top: 0.5em;
    left: 0.5em;
    right: 0.5em;
    bottom: 3em;
    overflow: auto;
}

#form {
    padding: 0 0.5em 0 0.5em;
    margin: 0;
    position: absolute;
    bottom: 1em;
    left: 0px;
    width: 100%;
    overflow: hidden;
}

</style>
</head>
<body>
<div id="log"></div>
<form id="form">
    <input type="submit" value="Send" />
    <input type="text" id="msg" size="64"/>
</form>
</body>
</html> `
