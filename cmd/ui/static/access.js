var selected_group = 0;

$(document).ready(function() {
    $("body").keypress(keypressHandler);
    $("#access-group-selection").change(function(e) {
	window.location.href = "/access/" + $(this).val();
    });
    $("#button-update").click(update);
    // $("table#acl-rules input.checked-rules").change(function() {checkedRulesChanged($(this))});
    //changeSelected(1);
});

function update() {
    var active = new Array;
    var comments = new Array;
    $(".access-acl-checked:checked").each(function(index) {
	var aclid = $(this).data("aclid");
	active[index] = aclid;
	comments[index] = $("#access-comment-" + aclid).val();
    });
    var data = {};
    data["acls"] = active;
    data["comments"] = comments;
    doPost("/access/" + $("#access-group-selection").val(),
	   data,
	   function() {
	       console.log("success");
	   });
}

function keypressHandler(event) {
}
