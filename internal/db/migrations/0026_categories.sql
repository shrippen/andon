-- Rules of a category (caps.Category) are written once and named after
-- it: inventory.* (Snipe-IT, Kept), bank.* (Sure, Firefly III). Hints keep
-- their notes and history; only the rule and text keys move. A space's
-- rule settings move along; where Sure and Firefly both had one, Sure's
-- wins (it comes last).

UPDATE hints SET rule = 'inventory.warranty_expiring' WHERE rule = 'snipe.warranty_expiring';
UPDATE hints SET rule = 'inventory.gwg_hint' WHERE rule = 'snipe.gwg_hint';
UPDATE hints SET rule = 'inventory.expense_missing' WHERE rule = 'snipe.expense_missing';
UPDATE hints SET rule = 'inventory.replace_worth' WHERE rule = 'snipe.replace_worth';
UPDATE hints SET rule = 'bank.recurring_missed' WHERE rule = 'firefly.recurring_missed';
UPDATE hints SET rule = 'bank.low_balance' WHERE rule = 'firefly.low_balance';
UPDATE hints SET rule = 'bank.uncategorized' WHERE rule = 'firefly.uncategorized';
UPDATE hints SET rule = 'bank.unusual_expense' WHERE rule = 'firefly.unusual_expense';
UPDATE hints SET rule = 'bank.recurring_missed' WHERE rule = 'sure.recurring_missed';
UPDATE hints SET rule = 'bank.low_balance' WHERE rule = 'sure.low_balance';
UPDATE hints SET rule = 'bank.uncategorized' WHERE rule = 'sure.uncategorized';
UPDATE hints SET rule = 'bank.unusual_expense' WHERE rule = 'sure.unusual_expense';
UPDATE hints SET rule = 'bank.subscription_unused' WHERE rule = 'sure.subscription_unused';
UPDATE hints SET rule = 'bank.spendable_negative' WHERE rule = 'sure.spendable_negative';
UPDATE hints SET message = 'inventory.warranty' WHERE message = 'snipe.warranty';
UPDATE hints SET message = 'inventory.gwg' WHERE message = 'snipe.gwg';
UPDATE hints SET message = 'inventory.expense_missing' WHERE message = 'snipe.expense_missing';
UPDATE hints SET message = 'inventory.replace_worth' WHERE message = 'snipe.replace_worth';
UPDATE hints SET message = 'bank.missed' WHERE message = 'sure.missed';
UPDATE hints SET message = 'bank.low' WHERE message = 'sure.low';
UPDATE hints SET message = 'bank.uncategorized' WHERE message = 'sure.uncategorized';
UPDATE hints SET message = 'bank.unusual' WHERE message = 'sure.unusual';
UPDATE hints SET message = 'bank.subscription_unused' WHERE message = 'sure.subscription_unused';
UPDATE hints SET message = 'bank.subscription_unused_deadline' WHERE message = 'sure.subscription_unused_deadline';
UPDATE hints SET message = 'bank.spendable_negative' WHERE message = 'sure.spendable_negative';

UPDATE spaces SET settings = json_set(json_remove(settings, '$.rules."snipe.warranty_expiring"'), '$.rules."inventory.warranty_expiring"', json(settings -> '$.rules."snipe.warranty_expiring"'))
    WHERE json_valid(settings) AND settings -> '$.rules."snipe.warranty_expiring"' IS NOT NULL;
UPDATE spaces SET settings = json_set(json_remove(settings, '$.rules."snipe.gwg_hint"'), '$.rules."inventory.gwg_hint"', json(settings -> '$.rules."snipe.gwg_hint"'))
    WHERE json_valid(settings) AND settings -> '$.rules."snipe.gwg_hint"' IS NOT NULL;
UPDATE spaces SET settings = json_set(json_remove(settings, '$.rules."snipe.expense_missing"'), '$.rules."inventory.expense_missing"', json(settings -> '$.rules."snipe.expense_missing"'))
    WHERE json_valid(settings) AND settings -> '$.rules."snipe.expense_missing"' IS NOT NULL;
UPDATE spaces SET settings = json_set(json_remove(settings, '$.rules."snipe.replace_worth"'), '$.rules."inventory.replace_worth"', json(settings -> '$.rules."snipe.replace_worth"'))
    WHERE json_valid(settings) AND settings -> '$.rules."snipe.replace_worth"' IS NOT NULL;
UPDATE spaces SET settings = json_set(json_remove(settings, '$.rules."firefly.recurring_missed"'), '$.rules."bank.recurring_missed"', json(settings -> '$.rules."firefly.recurring_missed"'))
    WHERE json_valid(settings) AND settings -> '$.rules."firefly.recurring_missed"' IS NOT NULL;
UPDATE spaces SET settings = json_set(json_remove(settings, '$.rules."firefly.low_balance"'), '$.rules."bank.low_balance"', json(settings -> '$.rules."firefly.low_balance"'))
    WHERE json_valid(settings) AND settings -> '$.rules."firefly.low_balance"' IS NOT NULL;
UPDATE spaces SET settings = json_set(json_remove(settings, '$.rules."firefly.uncategorized"'), '$.rules."bank.uncategorized"', json(settings -> '$.rules."firefly.uncategorized"'))
    WHERE json_valid(settings) AND settings -> '$.rules."firefly.uncategorized"' IS NOT NULL;
UPDATE spaces SET settings = json_set(json_remove(settings, '$.rules."firefly.unusual_expense"'), '$.rules."bank.unusual_expense"', json(settings -> '$.rules."firefly.unusual_expense"'))
    WHERE json_valid(settings) AND settings -> '$.rules."firefly.unusual_expense"' IS NOT NULL;
UPDATE spaces SET settings = json_set(json_remove(settings, '$.rules."sure.recurring_missed"'), '$.rules."bank.recurring_missed"', json(settings -> '$.rules."sure.recurring_missed"'))
    WHERE json_valid(settings) AND settings -> '$.rules."sure.recurring_missed"' IS NOT NULL;
UPDATE spaces SET settings = json_set(json_remove(settings, '$.rules."sure.low_balance"'), '$.rules."bank.low_balance"', json(settings -> '$.rules."sure.low_balance"'))
    WHERE json_valid(settings) AND settings -> '$.rules."sure.low_balance"' IS NOT NULL;
UPDATE spaces SET settings = json_set(json_remove(settings, '$.rules."sure.uncategorized"'), '$.rules."bank.uncategorized"', json(settings -> '$.rules."sure.uncategorized"'))
    WHERE json_valid(settings) AND settings -> '$.rules."sure.uncategorized"' IS NOT NULL;
UPDATE spaces SET settings = json_set(json_remove(settings, '$.rules."sure.unusual_expense"'), '$.rules."bank.unusual_expense"', json(settings -> '$.rules."sure.unusual_expense"'))
    WHERE json_valid(settings) AND settings -> '$.rules."sure.unusual_expense"' IS NOT NULL;
UPDATE spaces SET settings = json_set(json_remove(settings, '$.rules."sure.subscription_unused"'), '$.rules."bank.subscription_unused"', json(settings -> '$.rules."sure.subscription_unused"'))
    WHERE json_valid(settings) AND settings -> '$.rules."sure.subscription_unused"' IS NOT NULL;
UPDATE spaces SET settings = json_set(json_remove(settings, '$.rules."sure.spendable_negative"'), '$.rules."bank.spendable_negative"', json(settings -> '$.rules."sure.spendable_negative"'))
    WHERE json_valid(settings) AND settings -> '$.rules."sure.spendable_negative"' IS NOT NULL;
