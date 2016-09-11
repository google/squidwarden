var selected_rule = 0;
var editing = false;

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

    // Rule selection.
    $("#acl-rules input.checked-rules").change(function() { checkedRulesChanged($(this)); });
    changeSelected(0);

    // Rule editing.
    var f = function() { ruleTextChanged($(this)); }
    $("#acl-rules input[type=text],#acl-rules select").change(f);
    $("#acl-rules input[type=text]").keydown(f);
    $("#button-save").click(save);
});

function get_ruleid_by_index(n) {
    return $("#acl-rules tbody tr:nth-child("+(selected_rule+1)+") input.checked-rules").data("ruleid");
}

function save() {
    var id = get_ruleid_by_index(selected_rule);
    console.log(id);
}

function ruleTextChanged(me) {
    var ruleid = me.data("ruleid");

    // Remove all checkmarks.
    $("#acl-rules input.checked-rules").prop("checked", false);
    $("#acl-rules tr").removeClass("selected");
    $("#button-move").attr("disabled", "disabled");

    // Disable all but active rule for editing.
    $("#acl-rules input,#acl-rules select").each(function(index) {
	if ($(this).data("ruleid") !== ruleid) {
	    $(this).attr("disabled", "disabled");
	}
    });

    // Set active.
    $("#acl-rules input.checked-rules").each(function(index) {
	if ($(this).data("ruleid") === ruleid) {
	    changeSelected(index-selected_rule);
	}
    });

    // Enable save button.
    $("#button-save").removeAttr("disabled");

    // Disable active-changing.
    editing = true;
}

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

function keyCheck() {
    var o = $("#acl-rules tbody tr:nth-child("+(selected_rule+1)+") input.checked-rules");
    o.prop("checked", !o.prop("checked"));
    checkedRulesChanged(o);

    o = $("#button-move");
    if ($(".checked-rules:checked").length > 0) {
	o.removeAttr("disabled");
    } else {
	o.attr("disabled", "disabled");
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
	keyCheck();
	break;
    default:
	console.log("Keypress: " + event.which);
	break;
    }
}

function changeSelected(delta) {
    if (editing) {
	return;
    }
    var o;
    $("#acl-rules tbody tr:nth-child("+(selected_rule+1)+") td.acl-rules-row-selected").text("");
    selected_rule += delta;
    if (selected_rule < 0) {
	selected_rule = 0;
    }
    var c = $("#acl-rules tbody tr").length - 1;
    if (selected_rule >= c) {
	selected_rule = c;
    }
    o = $("#acl-rules tbody tr:nth-child("+(selected_rule+1)+") td.acl-rules-row-selected");
    o.text(">");
    var screen_pos = o[0].getBoundingClientRect().top;
    var delta = 0;
    var min = 20;
    var max = window.innerHeight - 50;
    if (screen_pos < min) {
	delta = screen_pos - min;
    } else if (screen_pos > max) {
	delta = screen_pos - max;
    }
    window.scroll(0, window.scrollY+delta);
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
