var selected_rule = 0;

$(document).ready(function() {
    $("#acl-selection").change(function(e) {
	window.location.href = "/acl/" + $(this).val();
    });
    $("#button-move").click(move);

    $("body").keypress(keypressHandler);
    $("#new-acl").keydown(function(e) {
	if (e.keyCode != 13) { return; }
	newACL($(this).val());
    });
    $("table#acl-rules input.checked-rules").change(function() {checkedRulesChanged($(this))});
    changeSelected(0);
});

function newACL(name) {
    $.post("/acl/new", {"comment": name})
	.done(function() {
	    console.log("success");
	}).fail(function(o, text, error) {
	    console.log("Failed!");
	});
}

function checkedRulesChanged(me) {
    var ruleid = me.data("ruleid");
    if (me.prop("checked")) {
	$("#acl-rules-row-"+ruleid).addClass("selected");
    } else {
	$("#acl-rules-row-"+ruleid).removeClass("selected");
    }
}

function keypressHandler(event) {
    switch (event.which) {
    case 106: // 'j'
	changeSelected(1);
	break;
    case 107: // 'k'
	changeSelected(-1);
	break;
    case 120: // 'x'
	var o = $("#acl-rules tbody tr:nth-child("+(selected_rule+1)+") input.checked-rules");
	o.prop("checked", !o.prop("checked"));
	checkedRulesChanged(o);
	break;
    default:
	console.log("Keypress: " + event.which);
	break;
    }
}

function changeSelected(delta) {
    var o;
    $("#acl-rules tbody tr:nth-child("+(selected_rule+1)+") td.acl-rules-row-selected").text("");
    selected_rule += delta;
    if (selected_rule < 0) {
	selected_rule = 0;
    }
    $("#acl-rules tbody tr:nth-child("+(selected_rule+1)+") td.acl-rules-row-selected").text(">");
}

function move() {
    var rules = new Array;
    $(".checked-rules:checked").each(function(index) {
	rules[index] = $(this).data("ruleid");
    });
    var data = {};
    data["destination"] = $("#acl-move-selection").val();
    data["rules"] = rules;
    $.post("/acl/move", data)
	.done(function() {
	    console.log("success");
	    for (var i = 0; i < rules.length; i++) {
		$("#acl-rules-row-" + rules[i]).remove();
		changeSelected(0);
	    }
	}).fail(function(o, text, error) {
	    console.log("Failed!");
	});
}
