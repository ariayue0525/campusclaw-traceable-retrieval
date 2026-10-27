def run(c):
    teacher = c.values["teacher"]["access_token"]
    student_a = c.values["student_a"]["access_token"]
    student_b = c.values["student_b"]["access_token"]
    class_a = c.values["teacher"]["user"]["classId"]
    class_b = c.values["student_b"]["user"]["classId"]
    for token,class_id in ((student_a,class_a),(student_b,class_b)):
        status,_,builtins=c.request("GET","/api/agents",token=token)
        assert status==200 and {"启发式解题助手","概念讲解助手"}.issubset({a["name"] for a in builtins["agents"]})
        assert c.db_scalar(f"SELECT COUNT(*) FROM tutor_agents WHERE class_id={class_id} AND name IN ('启发式解题助手','概念讲解助手');") == "2"
    skill_a = int(c.db_scalar(f"SELECT id FROM skills WHERE class_id={class_a} ORDER BY id LIMIT 1;"))
    material_a = int(c.db_scalar(f"SELECT id FROM materials WHERE class_id={class_a} ORDER BY id LIMIT 1;"))
    material_b = int(c.db_scalar(f"SELECT id FROM materials WHERE class_id={class_b} ORDER BY id LIMIT 1;"))
    payload = {"name":c.unique("接口智能体"),"prompt":"用简短问题启发学生。","enabled":True,"skill_ids":[skill_a],"material_ids":[material_a]}
    status, _, denied = c.request("POST", "/api/agents", payload, student_a)
    assert status == 403 and "error" in denied
    status, _, body = c.request("POST", "/api/agents", payload, teacher)
    assert status == 201, (status, body)
    agent = body["agent"]
    assert agent["skill_ids"] == [skill_a] and agent["material_ids"] == [material_a]
    duplicate_ids = {**payload, "name":c.unique("重复绑定"), "skill_ids":[skill_a,skill_a]}
    status, _, repeated = c.request("POST", "/api/agents", duplicate_ids, teacher)
    assert status == 400 and repeated["error"] == "绑定编号不能重复"
    status, _, duplicate_name = c.request("POST", "/api/agents", payload, teacher)
    assert status == 400 and "同名" in duplicate_name["error"]
    before = c.db_scalar(f"SELECT prompt FROM tutor_agents WHERE id={agent['id']};")
    forbidden_update = {**payload,"prompt":"不应落库","material_ids":[material_b]}
    status, _, cross = c.request("PUT", f"/api/agents/{agent['id']}", forbidden_update, teacher)
    assert status == 404 and "error" in cross
    status, _, after_body = c.request("GET", f"/api/agents/{agent['id']}", token=teacher)
    assert status == 200 and after_body["agent"]["prompt"] == before and after_body["agent"]["material_ids"] == [material_a]
    status, _, other = c.request("GET", f"/api/agents/{agent['id']}", token=student_b)
    status2, _, missing = c.request("GET", "/api/agents/9223372036854770000", token=student_b)
    assert status == status2 == 404 and other == missing
    assert c.db_scalar(f"SELECT COUNT(*) FROM tutor_agent_skills WHERE agent_id={agent['id']} AND skill_id={skill_a};") == "1"
    disabled = {**payload,"enabled":False}
    status, _, updated = c.request("PUT", f"/api/agents/{agent['id']}", disabled, teacher)
    assert status == 200 and updated["agent"]["enabled"] is False
    status, _, hidden = c.request("GET", f"/api/agents/{agent['id']}", token=student_a)
    assert status == 404 and hidden == missing
    status, _, listing = c.request("GET", "/api/agents", token=student_a)
    assert status == 200 and all(item["id"] != agent["id"] for item in listing["agents"])
    return "学生写入 403；唯一/重复 ID 约束；跨班更新完整回滚；A/B 404 同形；停用后学生不可见"
