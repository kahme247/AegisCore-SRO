-- The original SRO_VT_SHARD has no FK from _RefSkill.GroupID to _RefSkillGroup.
-- Some valid legacy skills reference absent groups, so retain the index but do
-- not reject those source rows during an exact reference import.
ALTER TABLE `REF_SKILL` DROP FOREIGN KEY `fk_ref_skill_group`;
