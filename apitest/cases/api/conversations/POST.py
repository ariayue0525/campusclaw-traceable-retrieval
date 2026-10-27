def run(c):
    teacher = c.values["teacher"]["access_token"]
    student_a = c.values["student_a"]["access_token"]
    student_b = c.values["student_b"]["access_token"]
    class_a = c.values["student_a"]["user"]["classId"]
    user_a = c.values["student_a"]["user"]["id"]
    class_b = c.values["student_b"]["user"]["classId"]
    agent_a = int(c.db_scalar(f"SELECT id FROM tutor_agents WHERE class_id={class_a} AND enabled=TRUE ORDER BY id LIMIT 1;"))
    agent_b = int(c.db_scalar(f"SELECT id FROM tutor_agents WHERE class_id={class_b} AND enabled=TRUE ORDER BY id LIMIT 1;"))
    status, _, cross_agent = c.request("POST", "/api/conversations", {"agent_id":agent_b}, student_a)
    status2, _, absent_agent = c.request("POST", "/api/conversations", {"agent_id":9223372036854770000}, student_a)
    assert status == status2 == 404 and cross_agent == absent_agent
    status, _, tamper = c.request("POST", "/api/conversations", {"agent_id":agent_a,"user_id":999,"class_id":class_b}, student_a)
    assert status == 400 and "error" in tamper
    status, _, created = c.request("POST", "/api/conversations", {"agent_id":agent_a}, student_a)
    assert status == 201, (status,created)
    item = created["conversation"]
    assert item["agent_id"] == agent_a and item["class_id"] == class_a and item["user_id"] == user_a
    assert c.db_scalar(f"SELECT COUNT(*) FROM conversations WHERE id={item['id']} AND class_id={class_a} AND user_id={user_a} AND agent_id={agent_a};") == "1"
    status, _, own = c.request("GET", f"/api/conversations/{item['id']}", token=student_a)
    assert status == 200 and own["conversation"]["id"] == item["id"]
    status, _, empty_messages = c.request("GET", f"/api/conversations/{item['id']}/messages", token=student_a)
    assert status == 200 and empty_messages["messages"] == []
    for other in (teacher,student_b):
        status, _, hidden = c.request("GET", f"/api/conversations/{item['id']}", token=other)
        status2, _, missing = c.request("GET", "/api/conversations/9223372036854770000", token=other)
        assert status == status2 == 404 and hidden == missing
        status, _, hidden_messages = c.request("GET", f"/api/conversations/{item['id']}/messages", token=other)
        assert status == 404 and hidden_messages == missing
    status, _, listing = c.request("GET", "/api/conversations", token=student_a)
    assert status == 200 and any(x["id"] == item["id"] for x in listing["conversations"])
    return "会话创建锁定班级/用户/智能体；跨班智能体与不存在同形 404；他人会话和消息不可读"
