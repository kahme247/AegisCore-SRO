-- Historical listing item IDs may outlive runtime ITEM rows after buyback
-- rollover. Reinstating the foreign key would be data-destructive.
DO 0;
