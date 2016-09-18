var selected_group = 0;

$(document).ready(function() {
    //$("body").keypress(keypressHandler);
    $("#members-group-selection").change(function(e) {
	window.location.href = "/members/" + $(this).val();
    });
    $(".members-source-checked,.members-comment").change(changeAnything);
    $(".members-comment").keydown(changeAnything);
    $("#action-new-group").keydown(function(e) {
	if (e.keyCode != 13) { return; }
	newGroup($(this).val());
    });
    $("#action-delete-group").click(function() {
	var group_id = $("#group-delete-selection").val();
	doDelete("/group/" + group_id, {}, function(){window.location.reload();});
    });
    $("#action-save").click(btnSave);
    $("#action-new").click(btnCreate);
    $(".action-delete").click(btnDelete);
});

function btnDelete() {
    var sourceID = $(this).data("sourceid");
    doDelete("/source/" + sourceID, {}, function() {
	$("#members-row-"+sourceID).remove();
    });
}

function changeAnything() {
    $("#action-save").prop("disabled", false);

    $(".members-source-checked:checked").each(function() {
	var sid = $(this).data("sourceid");
	$(".members-comment[data-sourceid="+sid+"]").prop("disabled", false);
    });
    $(".members-source-checked:not(:checked)").each(function() {
	var sid = $(this).data("sourceid");
	$(".members-comment[data-sourceid="+sid+"]").prop("disabled", true);
    });
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

function newGroup(name) {
    doPost("/group/new",
	   {"comment": name},
	   function(resp) {
	       console.log("Success!", resp);
	       window.location.href = "/members/" + resp.group;
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
	       // Only allow delete for unchecked rules.
	       $(".members-source-checked:not(:checked)").each(function(index) {
		   var sid = $(this).data("sourceid");
		   $("button[data-sourceid="+sid+"]").prop("disabled", false);
		   $(".members-comment[data-sourceid="+sid+"]").val("");
		   $(".members-comment[data-sourceid="+sid+"]").prop("disabled", true);
	       });
	       $(".members-source-checked:checked").each(function(index) {
		   var sid = $(this).data("sourceid");
		   $("button[data-sourceid="+sid+"]").prop("disabled", true);
		   $(".members-comment[data-sourceid="+sid+"]").prop("disabled", false);
	       });
	   },
	   function(o, text, error) {
	       console.log("Update failed");
	   });
}
