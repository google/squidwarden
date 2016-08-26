$(document).ready(function() {
    $("#acl-selection").change(function(e) {
	window.location.href = "/acl/" + $(this).val();
    });
    $("#button-move").click(move);
});

function move() {
    var rules = new Array;
    $(".checked-rules:checked").each(function(index) {
	rules[index] = $(this).data("id");
    });
    var data = {};
    data["destination"] = $("#acl-move-selection").val();
    data["rules"] = rules;
    $.post("/acl/move", data)
	.done(function() {
	    console.log("success");
	    for (var i = 0; i < rules.length; i++) {
		$("#acl-rules-row-" + rules[i]).css("display", "none");
	    }
	}).fail(function(o, text, error) {
	    console.log("Failed!");
	});
}
