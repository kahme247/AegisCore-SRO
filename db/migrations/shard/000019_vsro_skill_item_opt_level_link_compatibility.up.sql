-- Link is not unique in the original _RefSkillByItemOptLevel table.
ALTER TABLE `REF_SKILL_BY_ITEM_OPT_LEVEL` DROP PRIMARY KEY, ADD KEY `idx_ref_skill_opt_level_link` (`LINK`);
