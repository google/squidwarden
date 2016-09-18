var selected_group = 0;

$(document).ready(function() {
    //$("body").keypress(keypressHandler);
    $("#members-group-selection").change(function(e) {
	window.location.href = "/members/" + $(this).val();
    });
    $(".members-source-checked,.members-comment").change(changeAnything);
    $(".members-comment").keydown(changeAnything);
    $("#action-save").click(btnSave);
    $("#action-new").click(btnCreate);
});

function loading(b) {
    if (b) {
	$("#loading").text("Loading...");
    } else {
	$("#loading").text("");
    }
}

function changeAnything() {
    $("#action-save").prop("disabled", "");
}

function btnCreate() {
    var group_id = $("#members-group-selection").val();
    doPost("/members/"+group_id+"/new", {
	"source": $("#new-member-addr").val(),
	"source-comment": $("#new-member-source").val(),
	"comment": $("#new-member-comment").val(),
    }, function() {
	console.log("Success!");
	window.location.reload();
    });
}

function btnSave() {
    var group_id = $("#members-group-selection").val();
    var sources = new Array;
    var comments = new Array;
    $(".members-source-checked:checked").each(function(index) {
	var sid = $(this).data("sourceid");
	sources[index] = sid;
	comments[index] = $(".members-comment[data-sourceid="+sid+"]").val();
    });
    var data = {
	"sources": sources,
	"comments": comments,
    };
    doPost("/members/" + group_id + "/members", data,
	   function() {
	       console.log("Update succeeded. Group now has", sources.length, "members");
	       $("#action-save").prop("disabled", true);
	   },
	   function(o, text, error) {
	       console.log("Update failed");
	   });
}

function doPost(url, data, success, fail) {
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
