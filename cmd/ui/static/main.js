$(document).ready(function() {
    if (window.WebSocket && $("#websockets").val() == "true") {
	$("button#refresh-tail").css("display", "none");
	$("button#pause-scroll").click(pauseScroll);
	streamTail();
    } else {
	$("button#pause-scroll").css("display", "none");
	$("button#refresh-tail").click(refreshTail);
	refreshTail();
    }
});

function openWebsocket(path) {
    if (window.location.protocol == "http:") {
	websocket = new WebSocket("ws://"+window.location.host+path);
    } else {
	websocket = new WebSocket("wss://"+window.location.host+path);
    }
    return websocket;
}

var wsTail;
function streamTail() {
    wsTail = openWebsocket("/ajax/tail-log/stream");
    wsTail.onopen = function(){console.log("Tail log open")}
    wsTail.onclose = function(ev){
	console.log("websocket closed with code " + ev.code + ", reopening...");
	streamTail();
    }
    wsTail.onerror = function(ev){
	error("Error streaming tail log. Will re-attempt");
    }
    wsTail.onmessage = function(evt) {
	var data = JSON.parse(evt.data);
	var l = $("#latest tbody");
	l.prepend(tailLogRow(data));
    }
}

function tailLogRow(data) {
    var tr;
    var td;
    var ul;
    tr = document.createElement("tr");

    td = document.createElement("td");
    td.classList = ["min"];
    td.innerText = data.Time
    tr.appendChild(td);

    var li;
    var button;
    var span;
    td = document.createElement("td");
    td.classList = ["min nostripe"];
    ul = document.createElement("ul");
    ul.classList = ["buttons"];
    type = (data.Method == "CONNECT") ? "https-domain" : "domain";
    ul.appendChild(createButtonLI("Domain", {type: type, value: data.Domain}, data.Domain));
    ul.appendChild(createButtonLI("Host", {type: type, value: data.Host}, data.Host));
    if (data.Method != "CONNECT") {
	ul.appendChild(createButtonLI("Path", {
	    type: "exact",
	    value: data.URL,
	}, data.URL));
    }
    td.appendChild(ul);
    tr.appendChild(td);

    td = document.createElement("td");
    td.classList = ["min"];
    td.innerText = data.Client
    tr.appendChild(td);

    td = document.createElement("td");
    td.classList = ["min"];
    td.innerText = data.Method
    tr.appendChild(td);

    td = document.createElement("td");
    td.classList = ["min"];
    td.innerText = data.Host
    tr.appendChild(td);

    td = document.createElement("td");
    td.innerText = data.Path
    td.classList = ["latest-path max"]
    tr.appendChild(td);

    return tr;
}

function pauseScroll() {
    var btn = $("button#pause-scroll");
    if (btn.data("paused") == true) {
	btn.data("paused", false);
	btn.text("Pause scroll");
	$("#latest tbody").html("");
	streamTail();
    } else {
	btn.data("paused", true);
	btn.text("Unpause scroll");
	wsTail.onclose = function(){}
	wsTail.close();
    }
}

function refreshTail() {
    var l = $("#latest tbody");
    l.html("");
    $.getJSON("/ajax/tail-log", function(data) {
        for (var i = 0; i < data.length; i++) {
	    l.append(tailLogRow(data[i]));
	}
    }).fail(function(o, text, error) {
	error("Error: " + ajaxError(o, text, error));
    });
}

function error(msg) {
    var e = document.createElement("p");
    e.innerText = msg;
    $("#error-messages").append(e);
}

function createButtonLI(name, data, tip) {
    button = document.createElement("button");
    button.innerText = name;
    button.squidwarden_data = data;
    button.onclick = buttonClick;
    li = document.createElement("li");
    span = document.createElement("span");
    span.classList = ["tooltip"]
    span.innerText = tip;
    li.appendChild(button);
    li.appendChild(span);
    return li;
}

function buttonClick(btn) {
    var data = $.extend({}, btn.target.squidwarden_data, {"action": $("#action").val()});
    doPost("/rule/new",
	   data,
           function(resp) {
	       $("#test").html("Added " + resp.rule);
           });
}

