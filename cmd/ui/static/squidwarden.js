function loading(b) {
    if (b) {
	$("#loading").text("Loading...");
    } else {
	$("#loading").text("");
    }
}

function doPost(url, data, success, fail) {
    loading(true);
    $.ajax({
	method: "POST",
	url: url,
	data: data,
	dataType: "json",
	"success": function(data, status, xhr) {
	    loading(false);
	    if (success != undefined) { success(data); }
	},
	"error": function(o, text, error) {
	    console.log("POST failed");
	    loading(false);
	    ajaxError(o, text, error);
	    if (fail != undefined) { fail(o, text, error); }
	}
    });
}

function doDelete(url, indata, success, fail) {
    // We need *some* data or the content-type doesn't get set.
    var data = $.extend({"dummy": ""}, indata);
    loading(true);
    $.ajax({
	method: "DELETE",
	url: url,
	data: data,
	"success": function() {
	    loading(false);
	    console.log("DELETE success", success);
	    if (success != undefined) { success(); }
	},
	"error": function(o, text, error) {
	    console.log("DELETE failed", o, text, error);
	    loading(false);
	    ajaxError(o, text, error);
	    if (fail != undefined) { fail(o, text, error); }
	}
    });
}

function ajaxError(o, text, error) {
    var title;
    var msg;
    var links = new Array;
    if (o.readyState == 0) {
	msg = "Network error";
	title = msg;
    } else if (o.readyState == 4) {
	title = error;
	msg = error;
	if ("responseJSON" in o) {
	    if ("error" in o.responseJSON) {
		msg = o.responseJSON.error;
	    }
	    if ("links" in o.responseJSON && o.responseJSON.links !== null) {
		links = o.responseJSON.links;
	    }
	}
    } else {
	title = "Unknown error";
	msg = "unknown error type for readyState " + o.readyState;
    }
    if (links.length > 0) {
	var o = $("#error-window-links");
	o.html("");
	for (var i = 0; i < links.length; i++) {
	    var a = $("<a></a>");
	    a.prop("href", links[i].link);
	    a.text(links[i].text);
	    var li = $("<li></li>");
	    li.append(a);
	    o.append(li);
	}
    }
    $("#error-window-title").text(title);
    $("#error-window-body").text(msg);
    $("#error-window").css("display", "block");
    $("#error-window-close").focus();
    return msg;
}

$(document).ready(function() {
    $.ajaxPrefilter(function (options, originalOptions, jqXHR) {
	jqXHR.setRequestHeader('X-CSRF-Token', $("#csrf").val());
    });
    $("#error-window-close").click(function(){
	$("#error-window").css("display", "none");
    });
});
