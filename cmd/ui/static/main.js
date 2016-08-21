$(document).ready(function() {
    refreshTail();
    $("#refresh-tail").click(refreshTail);
});

function refreshTail() {
    var l = $("#latest tbody");
    l.html("");
    $.getJSON("/ajax/tail-log", function(data) {
	var tr;
	var td;
	var ul;
        for (var i = 0; i < data.length; i++) {
	    tr = document.createElement("tr");

	    td = document.createElement("td");
	    td.innerText = data[i].Time
	    tr.appendChild(td);

	    td = document.createElement("td");
	    td.innerText = data[i].Client
	    tr.appendChild(td);

	    td = document.createElement("td");
	    td.innerText = data[i].Method
	    tr.appendChild(td);

	    td = document.createElement("td");
	    td.innerText = data[i].Host
	    tr.appendChild(td);

	    td = document.createElement("td");
	    td.innerText = data[i].Path
	    td.classList = ["latest-path"]
	    tr.appendChild(td);

	    var li;
	    var button;
	    var span;
	    td = document.createElement("td");
	    ul = document.createElement("ul");
	    ul.classList = ["buttons"];
	    type = (data[i].Method == "CONNECT") ? "https-domain" : "domain";
	    ul.appendChild(createButtonLI("Domain", {type: type, value: data[i].Domain}, data[i].Domain));
	    ul.appendChild(createButtonLI("Host", {type: type, value: data[i].Host}, data[i].Host));
	    if (data[i].Method != "CONNECT") {
		ul.appendChild(createButtonLI("Path", {}, data[i].URL));
	    }

	    td.appendChild(ul);
	    tr.appendChild(td);

	    l.append(tr);
	}
    }).fail(function(o, text, error) {
	var msg;
	if (o.readyState == 0) {
	    msg = "Network error";
	} else if (o.readyState == 4) {
	    msg = text + ", " + error;
	} else {
	    msg = "unknown error type for readyState " + o.readyState;
	}
	var e = document.createElement("p");
	e.innerText = "Error: " + msg;
	$("#error-messages").append(e);
    });
}

function createButtonLI(name, data, value) {
    button = document.createElement("button");
    button.innerText = name;
    button.squidwarden_data = data;
    button.onclick = buttonClick;
    li = document.createElement("li");
    span = document.createElement("span");
    span.classList = ["tooltip"]
    span.innerText = value;
    li.appendChild(button);
    li.appendChild(span);
    return li;
}

function buttonClick(btn) {
    $("#test").html("Loading...");
    var data = btn.target.squidwarden_data;
    console.log(btn);
    console.log(data);
    $.post("/ajax/allow", data)
        .done(function(){
	    $("#test").html("Added");
        })
        .fail(function(){
	    $("#test").html("Failed!");
        });
}
