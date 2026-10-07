#!/usr/bin/env python3
"""Verify exact simulated/submitted return data, CPI state and rollback.

Derived from the preserved Phoenix adapter; requires return-data metadata support.
"""
import argparse
import base64
import hashlib
import json
from pathlib import Path
import subprocess
import time

def account(state):
    return dict(data=[base64.b64encode(bytes.fromhex(state['data'])).decode(),'base64'],owner=state['owner'],
        lamports=state['lamports'],executable=state['executable'],rentEpoch=state['rent_epoch'])
def compare(actual,expected,label):
    assert actual is not None,label+': missing account'
    data=actual['data']
    if isinstance(data,list):data=data[0]
    assert base64.b64decode(data).hex()==expected['data'],label+': data bytes'
    for key in ('owner','lamports','executable'):
        assert actual[key]==expected[key],label+': '+key
    assert actual['rentEpoch']==expected['rent_epoch'],label+': rent epoch'

def main():
    p=argparse.ArgumentParser(description=__doc__)
    for flag in ('runner','maker','elf','fixtures','features','report'):p.add_argument('--'+flag,required=True,type=Path)
    p.add_argument('--limit',type=int,default=1400000)
    p.add_argument('--record-errors',type=Path,help='Record upstream errors for marked failure cases; still verify all rollback bytes')
    args=p.parse_args()
    fixture=json.loads(args.fixtures.read_text());program=fixture['program_id']
    address={a['name']:a['address'] for a in fixture['accounts']};address['program']=program
    address.update({a['name']:a['address'] for a in fixture.get('programs',[])})
    version=subprocess.check_output([str(args.runner),'--version'],text=True).strip()
    assert version=='gosvm-svm-runner 0.5.0 (LiteSVM 0.8.2 + rent-error patch)',version
    runner=subprocess.Popen([str(args.runner),'--stdio'],stdin=subprocess.PIPE,stdout=subprocess.PIPE,text=True)
    maker=subprocess.Popen([str(args.maker)],stdin=subprocess.PIPE,stdout=subprocess.PIPE,text=True)
    identity=0
    def request(op,**body):
        nonlocal identity
        frame=dict(schema=1,id=identity,op=op,**body);identity+=1
        runner.stdin.write(json.dumps(frame,separators=(',',':'))+'\n');runner.stdin.flush()
        reply=json.loads(runner.stdout.readline())
        assert reply['schema']==1 and reply['id']==frame['id'],'protocol identity'
        assert not reply.get('error'),reply.get('error')
        return reply['result']
    def rpc(calls):
        results=request('batch',calls=[dict(jsonrpc='2.0',id=i,method=m,params=v) for i,(m,v) in enumerate(calls)])
        assert len(results)==len(calls),'batch length'
        for i,value in enumerate(results):
            assert value['id']==i and not value.get('error'),value
        return [x['result'] for x in results]
    def encode(body):
        maker.stdin.write(json.dumps(body)+'\n');maker.stdin.flush();return json.loads(maker.stdout.readline())
    started=time.perf_counter();rows=[]
    report=dict(passed=False,cases=rows,compute_unit_limit=args.limit,
        elf_sha256=hashlib.sha256(args.elf.read_bytes()).hexdigest(),
        fixtures_sha256=hashlib.sha256(args.fixtures.read_bytes()).hexdigest(),
        runner_sha256=hashlib.sha256(args.runner.read_bytes()).hexdigest(),
        transaction_maker_sha256=hashlib.sha256(args.maker.read_bytes()).hexdigest(),
        features=json.loads(args.features.read_text()),runtime_version=version)
    try:
        # Obtain the public fixture payer through the signer; no transaction is
        # sent by this call. This also validates the fixed test seed.
        payer=encode(dict(payer_seed=fixture['payer_seed'],blockhash=program,limit=0,instructions=[]))['payer']
        initial=[dict(pubkey=a['address'],account=account(a['initial'])) for a in fixture['accounts'] if 'initial' in a]
        programs=[dict(id=program,elf=base64.b64encode(args.elf.read_bytes()).decode())]
        for other in fixture.get('programs',[]):
            data=(args.fixtures.parent/other['elf']).read_bytes()
            assert hashlib.sha256(data).hexdigest()==other['sha256'],'callee ELF drift'
            programs.append(dict(id=other['address'],elf=base64.b64encode(data).decode()))
        request('init',config=dict(features=report['features'],payer=payer,accounts=initial,programs=programs))
        report['startup_seconds']=time.perf_counter()-started
        execution=time.perf_counter()
        for case in fixture['cases']:
            request('reset',accounts=[dict(pubkey=address[x['name']],account=account(x['state'])) for x in case.get('overrides',[])])
            row=dict(name=case['name'],steps=[]);rows.append(row)
            for step in case['steps']:
                if 'sysvars' in step:request('set_sysvars',sysvars=step['sysvars'])
                blockhash=rpc([('getLatestBlockhash',[])])[0]['value']['blockhash']
                instructions=[dict(program=address[ix['program']],data=ix['data'],
                    accounts=[dict(address=address[a['account']],writable=a['writable'],signer=a['signer']) for a in ix['accounts']]) for ix in step['instructions']]
                tx=encode(dict(payer_seed=fixture['payer_seed'],signer_seeds=fixture.get('signer_seeds',[]),blockhash=blockhash,limit=args.limit,instructions=instructions))
                checks=step['expect']['accounts'];addresses=[address[x['account']] for x in checks]
                expected=step['expect']['error']
                if expected is not None:
                    # The budget prefix occupies instruction index zero.
                    expected=json.loads(json.dumps(expected));expected['InstructionError'][0]+=1
                sim=rpc([('simulateTransaction',[tx['transaction'],dict(encoding='base64',sigVerify=True,accounts=dict(addresses=addresses))])])[0]['value']
                row['steps'].append(dict(cu=sim['unitsConsumed'],error=sim['err'],logs=sim['logs'],committed=False))
                expected_return=step['expect']['return_data']
                assert 'returnData' in sim,'runner lacks return-data metadata'
                assert sim['returnData']==expected_return,case['name']+': simulated return bytes/owner'
                row['steps'][-1]['return_data']=sim['returnData']
                if 'token_invocations' in step:
                    count=sum(line.startswith('Program TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA invoke [') for line in sim['logs'])
                    assert count==step['token_invocations'],f"{case['name']}: {count} token invocations"
                if step.get('reference_error',False):
                    assert args.record_errors,'marked upstream error needs --record-errors'
                    assert sim['err'] is not None,'upstream failure case unexpectedly succeeded'
                    expected=sim['err']
                    recorded=json.loads(json.dumps(expected));recorded['InstructionError'][0]-=1
                    step['expect']['error']=recorded
                    del step['reference_error']
                assert sim['err']==expected,f"{case['name']}: error {sim['err']}, expected {expected}"
                if expected is None:
                    assert len(sim['accounts'])==len(checks),'simulation account count'
                    for check,value in zip(checks,sim['accounts']):compare(value,check['state'],case['name']+' simulation '+check['account'])
                calls=[('sendTransaction',[tx['transaction'],dict(encoding='base64',skipPreflight=True)]),
                       ('getSignatureStatuses',[[tx['signature']]])]+[('getAccountInfo',[a]) for a in addresses]+[('getTransaction',[tx['signature']])]
                submitted=rpc(calls)
                assert submitted[0]==tx['signature'],'signature mismatch'
                assert submitted[1]['value'][0]['err']==expected,'submitted error'
                for check,value in zip(checks,submitted[2:-1]):compare(value['value'],check['state'],case['name']+' committed '+check['account'])
                meta=submitted[-1]['meta']
                assert meta['err']==expected and meta['returnData']==expected_return,case['name']+': submitted return bytes/owner'
                assert meta['computeUnitsConsumed']==sim['unitsConsumed'] and meta['logMessages']==sim['logs'],case['name']+': simulation/submission metadata'
                row['steps'][-1]['submitted_return_data']=meta['returnData']
                row['steps'][-1]['committed']=True
        report['fixture_seconds']=time.perf_counter()-execution
        report['runtime']=rpc([('gosvmRuntimeInfo',[])])[0]
        report['passed']=True
        if args.record_errors:args.record_errors.write_text(json.dumps(fixture,separators=(',',':'))+'\n')
        print(f'{len(rows)} scenarios passed with {args.limit} CU limit',flush=True)
    finally:
        runner.stdin.close();maker.stdin.close()
        for proc in (runner,maker):
            try:proc.wait(timeout=2)
            except subprocess.TimeoutExpired:proc.kill();proc.wait()
        report['total_seconds']=time.perf_counter()-started
        args.report.write_text(json.dumps(report,indent=2)+'\n')

if __name__=='__main__':main()
