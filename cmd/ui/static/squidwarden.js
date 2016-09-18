function loading(b) {
    if (b) {
	$("#loading").text("Loading...");
    } else {
	$("#loading").text("");
    }
}

function doPost(url, indata, success, fail) {
    var data = $.extend({"csrf": $("#csrf").val()}, indata); 
    loading(true);
    $.post(url, data)
	.done(function() {
	    loading(false);
	    if (success != undefined) { success(); }
	})
	.fail(function(o, text, error) {
	    console.log("POST failed");
	    loading(false);
	    if (fail != undefined) { fail(o, text, error); }
	});
}

function doDelete(url, indata, success, fail) {
    var data = $.extend({"csrf": $("#csrf").val()}, indata); 
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
