def run(c):
    tables = ["tutor_agents","skills","tutor_agent_skills","tutor_agent_materials","conversations","messages","prompt_templates","mcp_servers","mcp_tools","mcp_tool_calls"]
    for table in tables:
        assert c.db_scalar(f"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name='{table}';") == "1"
    for index in ["uq_agent_class_name","idx_agents_class_enabled","uq_skill_class_name","idx_skills_class_enabled","idx_conversations_owner_updated","idx_messages_conversation"]:
        assert c.db_scalar(f"SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema=DATABASE() AND index_name='{index}';") != "0", index
    foreign_keys = int(c.db_scalar("SELECT COUNT(*) FROM information_schema.referential_constraints WHERE constraint_schema=DATABASE() AND table_name IN ('tutor_agents','skills','tutor_agent_skills','tutor_agent_materials','conversations','messages');"))
    assert foreign_keys >= 10, foreign_keys
    assert c.db_scalar("SELECT COUNT(*) FROM skills WHERE seed_key LIKE 'lesson5-guidance-%';") == "2"
    assert c.db_scalar("SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='tutor_agents' AND column_name='prompt_template_id';") == "1"
    for index in ["uq_template_class_name", "uq_mcp_server_class_name", "uq_mcp_tool_server_name", "idx_mcp_calls_class_created"]:
        assert c.db_scalar(f"SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema=DATABASE() AND index_name='{index}';") != "0", index
    return f"十张表、迁移列、必要唯一/查询索引、{foreign_keys} 个旧外键及两班幂等技能种子均存在"
