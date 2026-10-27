import os
import subprocess

def run(c):
    command=['docker','run','--rm','--network','campusclaw_internal','--env-file','.env','-e','MYSQL_HOST=db','-e','RUN_DB_INTEGRATION=1','campusclaw-platform-test','go','test','./internal/server','-run','TestSSEPersistsIncrementalGatewayAnswer','-v','-count=1']
    result=subprocess.run(command,text=True,capture_output=True,timeout=180)
    assert result.returncode==0,(result.stdout[-1200:],result.stderr[-1200:])
    assert '--- PASS: TestSSEPersistsIncrementalGatewayAnswer' in result.stdout,result.stdout[-1200:]
    return '真实网关流解析：多个 delta 拼接与数据库持久化一致、meta 先于正文、模板进入系统消息'
