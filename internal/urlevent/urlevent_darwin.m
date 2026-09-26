//go:build darwin && cgo

#import <Cocoa/Cocoa.h>
#import <objc/runtime.h>

extern void cpmURLOpened(char *url);

static void cpmDeliver(NSString *url) {
	if (url != nil) {
		cpmURLOpened((char *)[url UTF8String]);
	}
}

@interface CPMURLEventHandler : NSObject
- (void)handleGetURLEvent:(NSAppleEventDescriptor *)event withReplyEvent:(NSAppleEventDescriptor *)reply;
@end

@implementation CPMURLEventHandler
- (void)handleGetURLEvent:(NSAppleEventDescriptor *)event withReplyEvent:(NSAppleEventDescriptor *)reply {
	cpmDeliver([[event paramDescriptorForKeyword:keyDirectObject] stringValue]);
}
@end

// application:openURLs: added to GLFW's app delegate. When the delegate
// implements it, AppKit routes kAEGetURL there instead of to a handler
// registered with NSAppleEventManager, so both paths lead to cpmDeliver.
static void cpmOpenURLs(id self, SEL _cmd, NSApplication *app, NSArray<NSURL *> *urls) {
	for (NSURL *u in urls) {
		cpmDeliver([u absoluteString]);
	}
}

void cpmInstallURLHandler(void) {
	@autoreleasepool {
		static CPMURLEventHandler *handler;
		handler = [[CPMURLEventHandler alloc] init];
		[[NSAppleEventManager sharedAppleEventManager]
			setEventHandler:handler
				andSelector:@selector(handleGetURLEvent:withReplyEvent:)
			  forEventClass:kInternetEventClass
				 andEventID:kAEGetURL];
		Class delegate = objc_getClass("GLFWApplicationDelegate");
		if (delegate != Nil) {
			class_addMethod(delegate, @selector(application:openURLs:), (IMP)cpmOpenURLs, "v@:@@");
		}
	}
}
