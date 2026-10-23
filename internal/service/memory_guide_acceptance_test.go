package service

// Acceptance fixtures written independently of the delegated implementation.
import (
 "context"
 "encoding/json"
 "errors"
 "testing"

 "github.com/pentaqilo/netsidian/internal/domain"
 "github.com/pentaqilo/netsidian/internal/ports"
)

type guideJournalFault struct {
 ports.DocumentRepo
 kind domain.OperationKind
 failInsert bool
 failCommit bool
}
func (f *guideJournalFault) InsertOperation(ctx context.Context, op *domain.Operation) error {
 if f.failInsert&&op.Kind==f.kind {f.failInsert=false;return errors.New("injected final receipt interruption")}
 return f.DocumentRepo.InsertOperation(ctx,op)
}
func (f *guideJournalFault) SetOperationPhase(ctx context.Context,id string,phase domain.OperationPhase,detail string) error {
 op,err:=f.DocumentRepo.Operation(ctx,id);if err!=nil{return err}
 if f.failCommit&&op!=nil&&op.Kind==f.kind&&phase==domain.OpPhaseCommitted {f.failCommit=false;return errors.New("injected final receipt interruption")}
 return f.DocumentRepo.SetOperationPhase(ctx,id,phase,detail)
}
func guideRestart(s *Services)*Services{return NewServices(s.Cfg,s.Users,s.Spaces,s.Invites,s.Docs,s.Index,s.Mem,s.Prefs,s.Content,s.State)}

func TestGuideCorrectionHasCompleteHistoricalClaimIdentity(t *testing.T){
 h:=newHarness(t);ctx:=context.Background()
 old,err:=h.svc.RememberMemory(ctx,h.actor,h.space,&MemoryRememberInput{Key:"preference",Text:"Original wording",Kind:"preference",EvidenceClass:"user-stated"},"original");if err!=nil{t.Fatal(err)}
 correction,err:=h.svc.ReviseMemory(ctx,h.actor,h.space,&MemoryReviseInput{Key:"preference",Action:"supersede",Text:"New wording",ExpectedRevision:old.IndexRevision},"correction");if err!=nil{t.Fatal(err)}
 data,_:=json.Marshal(correction);var receipt map[string]any;_ =json.Unmarshal(data,&receipt)
 refs:=receipt["supersedes"].([]any);ref:=refs[0].(map[string]any)
 index,err:=h.svc.Docs.DocumentByNormKey(ctx,h.space,mustNormKey(MemoryIndexPath));if err!=nil{t.Fatal(err)}
 if ref["vaultId"]!=h.space||ref["indexDocumentId"]!=index.ID||ref["indexRevision"]!=old.IndexRevision||ref["key"]!="preference"{t.Fatalf("claim identity must survive another folder/vault using the same key: %s",data)}
 previous,err:=h.svc.RevisionContent(ctx,h.actor,h.space,index.ID,old.IndexRevision);if err!=nil||parseMemoryIndex(previous)[0].text!="Original wording"{t.Fatalf("historical claim lost: %q %v",previous,err)}
}

func TestGuideConsolidationRequiresPinnedSourceRevision(t *testing.T){
 h:=newHarness(t);ctx:=context.Background();_,err:=h.svc.RememberMemory(ctx,h.actor,h.space,&MemoryRememberInput{Key:"fact",Text:"A fact"},"seed");if err!=nil{t.Fatal(err)}
 source,err:=h.svc.CreateNote(ctx,h.actor,h.space,"Evidence.md","Observed result","",false);if err!=nil{t.Fatal(err)}
 text:="Derived conclusion"
 _,err=h.svc.ConsolidateMemory(ctx,h.actor,h.space,&MemoryConsolidateInput{OperationID:"unpinned-source",Sources:[]MemoryConsolidateSource{{DocumentID:source.ID}},Targets:[]MemoryConsolidateTarget{{Path:"Summary.md",Markdown:&text}}})
 if err==nil{t.Fatal("consolidation accepted an unpinned source")}
 created,_:=h.svc.Docs.DocumentByNormKey(ctx,h.space,mustNormKey("Summary.md"));if created!=nil{t.Fatal("invalid source plan wrote its target")}
}

func TestGuideConsolidationResumesAfterFinalReceiptInterruption(t *testing.T){
 h:=newHarness(t);ctx:=context.Background();_,err:=h.svc.RememberMemory(ctx,h.actor,h.space,&MemoryRememberInput{Key:"fact",Text:"A fact"},"seed");if err!=nil{t.Fatal(err)}
 source,err:=h.svc.CreateNote(ctx,h.actor,h.space,"Evidence.md","Observed result","",false);if err!=nil{t.Fatal(err)}
 text:="Derived conclusion";plan:=&MemoryConsolidateInput{OperationID:"receipt-interruption",Sources:[]MemoryConsolidateSource{{DocumentID:source.ID,ExpectedRevision:source.CurrentRevision}},Targets:[]MemoryConsolidateTarget{{Path:"Summary.md",Markdown:&text}}}
 h.svc.Docs=&guideJournalFault{DocumentRepo:h.svc.Docs,kind:domain.OperationKind("memory_consolidate"),failCommit:true}
 first,err:=h.svc.ConsolidateMemory(ctx,h.actor,h.space,plan);if err!=nil&&first==nil{t.Fatalf("must return outcome/recovery after writes: %v",err)}
 doc,err:=h.svc.Docs.DocumentByNormKey(ctx,h.space,mustNormKey("Summary.md"));if err!=nil||doc==nil{t.Fatalf("target missing: %v",err)}
 before:=doc.CurrentRevision;h.svc=guideRestart(h.svc)
 receipt,err:=h.svc.ConsolidateMemory(ctx,h.actor,h.space,plan);if err!=nil||receipt==nil||!receipt.Verified{t.Fatalf("same plan must reconcile completion, not conflict with its own target: %+v %v",receipt,err)}
 after,_:=h.svc.Docs.Document(ctx,doc.ID);if after.CurrentRevision!=before{t.Fatal("replay duplicated a completed target write")}
}

func TestGuideRevisionReceiptLossDoesNotDestroyRetry(t *testing.T){
 h:=newHarness(t);ctx:=context.Background();old,err:=h.svc.RememberMemory(ctx,h.actor,h.space,&MemoryRememberInput{Key:"fact",Text:"Old"},"seed");if err!=nil{t.Fatal(err)}
 input:=&MemoryReviseInput{Key:"fact",Action:"supersede",Text:"New",ExpectedRevision:old.IndexRevision}
 h.svc.Docs=&guideJournalFault{DocumentRepo:h.svc.Docs,kind:domain.OperationKind("memory_revise"),failInsert:true}
 _,_=h.svc.ReviseMemory(ctx,h.actor,h.space,input,"revise-once")
 h.svc=guideRestart(h.svc)
 receipt,err:=h.svc.ReviseMemory(ctx,h.actor,h.space,input,"revise-once");if err!=nil||receipt==nil||!receipt.Verified{t.Fatalf("same logical request must recover its saved revision: %+v %v",receipt,err)}
 recalled,err:=h.svc.RecallMemory(ctx,h.actor,h.space,&MemoryRecallInput{Key:"fact"});if err!=nil||len(recalled.Entries)!=1||recalled.Entries[0].Text!="New"{t.Fatalf("lost correction: %+v %v",recalled,err)}
}

func TestGuidePinnedEvidenceCannotFabricateVerification(t *testing.T){
 h:=newHarness(t);ctx:=context.Background();source,err:=h.svc.CreateNote(ctx,h.actor,h.space,"Evidence.md","Actual observed output","",false);if err!=nil{t.Fatal(err)}
 _,err=h.svc.RememberMemory(ctx,h.actor,h.space,&MemoryRememberInput{Key:"false-quote",Text:"Claim",SourceRefs:[]MemorySourceRef{{Kind:"document",Target:source.ID,Revision:source.CurrentRevision,Quote:"This sentence does not occur in the source"}}},"false-quote")
 if err==nil{t.Fatal("source quote absent from the pinned body must not be marked verified")}
 _,err=h.svc.RememberMemory(ctx,h.actor,h.space,&MemoryRememberInput{Key:"fake-revision",Text:"Claim",SourceRefs:[]MemorySourceRef{{Kind:"document",Target:source.ID,Revision:"rev_does_not_exist"}}},"fake-revision")
 if err==nil{t.Fatal("fabricated revision must not receive a verified source receipt")}
}

func TestGuideHistoricalEvidenceRequiresHistoryGrant(t *testing.T){
 h:=newHarness(t);ctx:=context.Background();source,err:=h.svc.CreateNote(ctx,h.actor,h.space,"Evidence.md","Old evidence","",false);if err!=nil{t.Fatal(err)}
 _,err=h.svc.SaveContent(ctx,h.actor,h.space,source.ID,source.CurrentRevision,"New evidence","");if err!=nil{t.Fatal(err)}
 restricted:=*h.actor;restricted.Key=&ports.APIKeyRecord{SpaceIDs:[]string{h.space},Scopes:[]string{domain.ScopeDocumentsRead,domain.ScopeDocumentsWrite}}
 _,err=h.svc.RememberMemory(ctx,&restricted,h.space,&MemoryRememberInput{Key:"history",Text:"Old claim",SourceRefs:[]MemorySourceRef{{Kind:"document",Target:source.ID,Revision:source.CurrentRevision,Quote:"Old evidence"}}},"history-without-grant")
 if err==nil{t.Fatal("historical source body verification bypassed the history grant")}
}

func TestGuideInferenceNeedsEvidence(t *testing.T){
 h:=newHarness(t)
 _,err:=h.svc.RememberMemory(context.Background(),h.actor,h.space,&MemoryRememberInput{Key:"unsupported-inference",Text:"This proves everything",Kind:"inference",EvidenceClass:"inferred"},"inference")
 if err==nil{t.Fatal("an inference without supporting evidence must not become verified current knowledge")}
}

func TestGuideTemporalBoundariesAndScopeIdentity(t *testing.T){
 h:=newHarness(t);ctx:=context.Background()
 _,err:=h.svc.RememberMemory(ctx,h.actor,h.space,&MemoryRememberInput{Key:"interval",Text:"A dated fact",ScopeID:"Project-A",ValidFrom:"2026-10-22T10:00:00Z",ValidTo:"2026-10-22T11:00:00Z"},"interval");if err!=nil{t.Fatal(err)}
 atEnd,err:=h.svc.RecallMemory(ctx,h.actor,h.space,&MemoryRecallInput{Key:"interval",ValidOn:"2026-10-22T11:00:00Z"});if err!=nil||len(atEnd.Entries)!=0{t.Fatalf("validTo is exclusive: %+v %v",atEnd,err)}
 other,err:=h.svc.RecallMemory(ctx,h.actor,h.space,&MemoryRecallInput{ScopeID:"project-a"});if err!=nil||len(other.Entries)!=0{t.Fatalf("scope identities are exact, not case-folded labels: %+v %v",other,err)}
}

type guideCrashAfterTarget struct{ports.DocumentRepo;crash bool}
func(f *guideCrashAfterTarget)SetOperationPhase(ctx context.Context,id string,phase domain.OperationPhase,detail string)error{
 op,err:=f.DocumentRepo.Operation(ctx,id);if err!=nil{return err}
 if f.crash&&op!=nil&&op.Kind==domain.OperationKind("memory_consolidate")&&phase==domain.OpPhasePending{f.crash=false;panic("simulated process interruption after committed target")}
 return f.DocumentRepo.SetOperationPhase(ctx,id,phase,detail)
}
func TestGuideConsolidationCrashBeforeStepCheckpoint(t *testing.T){
 h:=newHarness(t);ctx:=context.Background();_,err:=h.svc.RememberMemory(ctx,h.actor,h.space,&MemoryRememberInput{Key:"fact",Text:"A fact"},"seed");if err!=nil{t.Fatal(err)}
 source,err:=h.svc.CreateNote(ctx,h.actor,h.space,"Evidence.md","Observed result","",false);if err!=nil{t.Fatal(err)}
 text:="Derived conclusion";plan:=&MemoryConsolidateInput{OperationID:"crash-after-target",Sources:[]MemoryConsolidateSource{{DocumentID:source.ID,ExpectedRevision:source.CurrentRevision}},Targets:[]MemoryConsolidateTarget{{Path:"Summary.md",Markdown:&text}}}
 h.svc.Docs=&guideCrashAfterTarget{DocumentRepo:h.svc.Docs,crash:true}
 func(){defer func(){if recover()==nil{t.Fatal("fault was not injected")}}();_,_=h.svc.ConsolidateMemory(ctx,h.actor,h.space,plan)}()
 doc,_:=h.svc.Docs.DocumentByNormKey(ctx,h.space,mustNormKey("Summary.md"));if doc==nil{t.Fatal("fault must occur after target commit")}
 before:=doc.CurrentRevision;h.svc=guideRestart(h.svc);h.svc.ReconcileOperations(ctx)
 receipt,err:=h.svc.ConsolidateMemory(ctx,h.actor,h.space,plan);if err!=nil||receipt==nil||!receipt.Verified{t.Fatalf("replay must reconcile the committed child operation when step checkpoint was lost: %+v %v",receipt,err)}
 after,_:=h.svc.Docs.Document(ctx,doc.ID);if after.CurrentRevision!=before{t.Fatal("crash replay repeated a committed target")}
}
