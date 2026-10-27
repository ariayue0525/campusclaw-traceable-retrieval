def run(c):
    teacher=c.values['teacher']['access_token']
    student=c.values['student_a']['access_token']
    class_id=c.values['teacher']['user']['classId']
    payload={'name':c.unique('apitest-mcp'),'base_url':'http://mcp-server:8091'}
    status,_,denied=c.request('POST','/api/mcp/servers',payload,student)
    assert status==403 and 'error' in denied
    status,_,created=c.request('POST','/api/mcp/servers',payload,teacher)
    assert status==201,(status,created)
    server=created['server']
    assert server['class_id']==class_id and server['name']==payload['name']
    assert c.db_scalar(f"SELECT COUNT(*) FROM mcp_servers WHERE id={server['id']} AND class_id={class_id};")=='1'
    status,_,body=c.request('GET',f"/api/mcp/servers/{server['id']}/tools",token=teacher)
    assert status==200 and isinstance(body['tools'],list)
    status,_,body=c.request('GET','/api/mcp/audit',token=teacher)
    assert status==200 and isinstance(body['calls'],list)
    return 'MCP 教师登记持久化、学生 403、工具及审计接口结构通过；实际工具调用由 Go 集成测试覆盖'
