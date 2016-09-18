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
	    alert(o.responseText);
	    loading(false);
	    if (fail != undefined) { fail(o, text, error); }
	}
    });
}

$(document).ready(function() {
    $.ajaxPrefilter(function (options, originalOptions, jqXHR) {
	jqXHR.setRequestHeader('X-CSRF-Token', $("#csrf").val());
    });
});
