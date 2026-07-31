-- A completed Open Market listing is historical state. Its item ID remains
-- useful to queries after buyback rollover deletes the runtime ITEM row.
ALTER TABLE `OPEN_MARKET_LISTING`
    DROP FOREIGN KEY `fk_open_market_item`;
